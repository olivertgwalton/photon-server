package jobs

import (
	"context"
	"log/slog"
	"maps"
	"slices"
	"sync"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
)

const (
	poll = 2 * time.Second
	// lease is how long a worker may be silent before its job is taken to be lost and is claimed
	// again, so every job writes its result idempotently. A running job's lease is renewed every
	// third of it, so a long one, as a conversion is, keeps it.
	lease = 10 * time.Minute
)

type Handler func(ctx context.Context, subject uuid.UUID) error

type queue interface {
	ClaimJobs(ctx context.Context, kinds []domain.JobKind, node uuid.UUID, lease time.Duration, limit int) ([]store.Job, error)
	CompleteJob(ctx context.Context, id int64) error
	FailJob(ctx context.Context, job store.Job, err error) error
	ExtendLease(ctx context.Context, id int64, lease time.Duration) error
}

// Worker runs queued jobs of the kinds it has handlers for, at most slots at a time.
type Worker struct {
	queue    queue
	log      *slog.Logger
	node     uuid.UUID
	slots    int
	handlers map[domain.JobKind]Handler
}

func NewWorker(q queue, log *slog.Logger, node uuid.UUID, slots int, handlers map[domain.JobKind]Handler) *Worker {
	return &Worker{queue: q, log: log, node: node, slots: slots, handlers: handlers}
}

func (w *Worker) Run(ctx context.Context) {
	kinds := slices.Sorted(maps.Keys(w.handlers))
	free := make(chan struct{}, w.slots)
	for range w.slots {
		free <- struct{}{}
	}
	var wg sync.WaitGroup
	defer wg.Wait()
	t := time.NewTicker(poll)
	defer t.Stop()
	for ctx.Err() == nil {
		if n := len(free); n > 0 {
			claimed, err := w.queue.ClaimJobs(ctx, kinds, w.node, lease, n)
			if err != nil && ctx.Err() == nil {
				w.log.WarnContext(ctx, "jobs not claimed", slog.Any("err", err))
			}
			for _, job := range claimed {
				<-free
				wg.Go(func() {
					defer func() { free <- struct{}{} }()
					w.run(ctx, job)
				})
			}
		}
		select {
		case <-ctx.Done():
		case <-t.C:
		}
	}
}

func (w *Worker) run(ctx context.Context, job store.Job) {
	log := w.log.With(slog.String("job", string(job.Kind)), slog.String("subject", job.Subject.String()))
	done := make(chan struct{})
	go w.renew(ctx, log, job.ID, done)
	err := w.handlers[job.Kind](ctx, job.Subject)
	close(done)
	// A job cut short by shutdown is left to the lease sweep rather than counted as a failure.
	if ctx.Err() != nil {
		return
	}
	if err != nil {
		log.WarnContext(ctx, "job failed", slog.Int("attempt", job.Attempts), slog.Any("err", err))
		err = w.queue.FailJob(ctx, job, err)
	} else {
		err = w.queue.CompleteJob(ctx, job.ID)
	}
	if err != nil {
		log.WarnContext(ctx, "job outcome not recorded", slog.Any("err", err))
	}
}

// renew keeps a job's lease until done is closed.
func (w *Worker) renew(ctx context.Context, log *slog.Logger, id int64, done <-chan struct{}) {
	t := time.NewTicker(lease / 3)
	defer t.Stop()
	for {
		select {
		case <-done:
			return
		case <-ctx.Done():
			return
		case <-t.C:
			if err := w.queue.ExtendLease(ctx, id, lease); err != nil && ctx.Err() == nil {
				log.WarnContext(ctx, "job lease not renewed", slog.Any("err", err))
			}
		}
	}
}
