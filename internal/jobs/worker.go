package jobs

import (
	"context"
	"errors"
	"log/slog"
	"maps"
	"slices"
	"sync"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

const (
	poll = 2 * time.Second
	// lease is how long a worker may be silent before its job is taken to be lost and is claimed
	// again, so every job writes its result idempotently. A running job's lease is renewed every
	// third of it, so a long one, as a conversion is, keeps it.
	lease = 10 * time.Minute
	// notNow is how long a job with no room to run waits before it is claimed again.
	notNow = 30 * time.Second
	// recordTimeout bounds writing a job's outcome once the worker is stopping.
	recordTimeout = 5 * time.Second
)

// ErrNotNow is a handler's answer for a job that cannot run on its node yet: it is queued again for
// later, by any node, with its attempt given back.
var ErrNotNow = errors.New("jobs: no room to run this job here now")

type Handler func(ctx context.Context, subject uuid.UUID) error

type queue interface {
	ClaimJobs(ctx context.Context, kinds []domain.JobKind, node uuid.UUID, lease time.Duration, limit int) ([]domain.Job, error)
	CompleteJob(ctx context.Context, id int64) error
	FailJob(ctx context.Context, job domain.Job, err error) (bool, error)
	ExtendLease(ctx context.Context, id int64, node uuid.UUID, lease time.Duration) error
	PostponeJob(ctx context.Context, job domain.Job, delay time.Duration) error
}

// teller is told as each job starts and ends.
type teller interface {
	Raise(ctx context.Context, e domain.Event)
	// JobEnded is told of a job that finished or gave up, once its outcome is recorded.
	JobEnded(ctx context.Context, kind domain.JobKind)
}

// Worker runs queued jobs of the kinds it has handlers for, at most slots at a time.
type Worker struct {
	queue    queue
	log      *slog.Logger
	node     uuid.UUID
	slots    int
	handlers map[domain.JobKind]Handler
	tell     teller
}

// NewWorker runs jobs with handlers, telling tell as each starts and ends.
func NewWorker(q queue, log *slog.Logger, node uuid.UUID, slots int, handlers map[domain.JobKind]Handler, tell teller) *Worker {
	return &Worker{queue: q, log: log, node: node, slots: slots, handlers: handlers, tell: tell}
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

func (w *Worker) run(ctx context.Context, job domain.Job) {
	log := w.log.With(slog.String("job", string(job.Kind)), slog.String("subject", job.Subject.String()))
	done := make(chan struct{})
	jobCtx, lose := context.WithCancelCause(ctx)
	defer lose(nil)
	go w.renew(ctx, log, job.ID, lose, done)
	w.tell.Raise(ctx, event(domain.EventJobStarted, job, nil))
	runErr := w.handlers[job.Kind](jobCtx, job.Subject)
	close(done)
	// Another node may hold it now, so what this run made of it is not the job's outcome.
	if errors.Is(context.Cause(jobCtx), domain.ErrLeaseLost) {
		log.WarnContext(ctx, "job stopped", slog.Any("err", domain.ErrLeaseLost))
		return
	}
	// The outcome is written even as the worker stops, so no job waits out its lease for a sweep.
	record, cancel := context.WithTimeout(context.WithoutCancel(ctx), recordTimeout)
	defer cancel()
	var err error
	ended := false
	switch {
	case runErr != nil && ctx.Err() != nil:
		// Cut short by shutdown, which is no fault of its subject's: any node takes it now.
		err = w.queue.PostponeJob(record, job, 0)
	case errors.Is(runErr, ErrNotNow):
		err = w.queue.PostponeJob(record, job, notNow)
	case runErr != nil:
		log.WarnContext(ctx, "job failed", slog.Int("attempt", job.Attempts), slog.Any("err", runErr))
		ended, err = w.queue.FailJob(record, job, runErr)
		kind := domain.EventJobFailed
		if ended {
			kind = domain.EventJobDead
		}
		w.tell.Raise(ctx, event(kind, job, runErr))
	default:
		err = w.queue.CompleteJob(record, job.ID)
		ended = true
		w.tell.Raise(ctx, event(domain.EventJobFinished, job, nil))
	}
	if err != nil {
		log.WarnContext(ctx, "job outcome not recorded", slog.Any("err", err))
		return
	}
	if ended {
		w.tell.JobEnded(record, job.Kind)
	}
}

// renew keeps a job's lease until done is closed, and stops the job through lose if the lease was
// lost all the same, as when the database was out of reach for longer than a lease.
func (w *Worker) renew(ctx context.Context, log *slog.Logger, id int64, lose context.CancelCauseFunc, done <-chan struct{}) {
	t := time.NewTicker(lease / 3)
	defer t.Stop()
	for {
		select {
		case <-done:
			return
		case <-ctx.Done():
			return
		case <-t.C:
			err := w.queue.ExtendLease(ctx, id, w.node, lease)
			switch {
			case errors.Is(err, domain.ErrLeaseLost):
				lose(err)
				return
			case err != nil && ctx.Err() == nil:
				log.WarnContext(ctx, "job lease not renewed", slog.Any("err", err))
			}
		}
	}
}

func event(kind domain.EventKind, job domain.Job, runErr error) domain.Event {
	e := domain.Event{Kind: kind, Details: map[string]any{
		"job_id": job.ID, "job_kind": job.Kind, "subject": job.Subject, "attempt": job.Attempts,
	}}
	e.Item, e.Library = job.About()
	if runErr != nil {
		e.Details["error"] = runErr.Error()
	}
	return e
}
