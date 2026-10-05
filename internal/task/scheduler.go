package task

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

const (
	leaseName = "scheduler"
	leaseTTL  = 15 * time.Second
	tick      = 5 * time.Second
)

type Task struct {
	Key      domain.TaskKey
	Triggers []Trigger
	Run      func(ctx context.Context) error
}

type stateStore interface {
	HoldLease(ctx context.Context, name string, node uuid.UUID, ttl time.Duration) (bool, error)
	TaskStarts(ctx context.Context) (map[domain.TaskKey]time.Time, error)
	TaskStarted(ctx context.Context, key domain.TaskKey, at time.Time) error
	TaskFinished(ctx context.Context, key domain.TaskKey, at time.Time, result domain.TaskResult, err error) error
}

// Scheduler runs due tasks on whichever node holds the scheduler lease. Each task runs alone, and
// losing the lease cancels whatever is running so two nodes never run one task at once.
type Scheduler struct {
	store stateStore
	log   *slog.Logger
	node  uuid.UUID
	tasks []Task
}

// node identifies this process to the other nodes of the cluster.
func NewScheduler(st stateStore, log *slog.Logger, node uuid.UUID, tasks ...Task) *Scheduler {
	return &Scheduler{store: st, log: log, node: node, tasks: tasks}
}

func (s *Scheduler) Run(ctx context.Context) {
	t := time.NewTicker(tick)
	defer t.Stop()
	for ctx.Err() == nil {
		if s.holdLease(ctx) {
			s.lead(ctx, t)
		}
		select {
		case <-ctx.Done():
		case <-t.C:
		}
	}
}

// lead runs due tasks for as long as this node keeps the lease, then cancels them and waits.
func (s *Scheduler) lead(ctx context.Context, t *time.Ticker) {
	ctx, resign := context.WithCancel(ctx)
	var wg sync.WaitGroup
	defer func() {
		resign()
		wg.Wait()
	}()
	var mu sync.Mutex
	running := map[domain.TaskKey]bool{}
	for {
		starts, err := s.store.TaskStarts(ctx)
		if err != nil {
			s.log.WarnContext(ctx, "task state not read", slog.Any("err", err))
		}
		now := time.Now()
		for _, task := range s.tasks {
			mu.Lock()
			busy := running[task.Key]
			mu.Unlock()
			if err != nil || busy || !isDue(task, starts[task.Key], now) {
				continue
			}
			if err := s.store.TaskStarted(ctx, task.Key, now); err != nil {
				s.log.WarnContext(ctx, "task not started", slog.String("task", string(task.Key)), slog.Any("err", err))
				continue
			}
			mu.Lock()
			running[task.Key] = true
			mu.Unlock()
			wg.Go(func() {
				s.run(ctx, task)
				mu.Lock()
				delete(running, task.Key)
				mu.Unlock()
			})
		}
		// A tick and a cancellation can be ready together, and select picks either.
		select {
		case <-ctx.Done():
		case <-t.C:
		}
		if ctx.Err() != nil || !s.holdLease(ctx) {
			return
		}
	}
}

func (s *Scheduler) holdLease(ctx context.Context) bool {
	held, err := s.store.HoldLease(ctx, leaseName, s.node, leaseTTL)
	if err != nil && ctx.Err() == nil {
		s.log.WarnContext(ctx, "scheduler lease not held", slog.Any("err", err))
	}
	return held && err == nil
}

func isDue(task Task, last, now time.Time) bool {
	for _, t := range task.Triggers {
		if t.due(last, now) {
			return true
		}
	}
	return false
}

func (s *Scheduler) run(ctx context.Context, task Task) {
	log := s.log.With(slog.String("task", string(task.Key)))
	log.InfoContext(ctx, "task started")
	err := task.Run(ctx)
	result := domain.TaskSucceeded
	switch {
	case errors.Is(err, context.Canceled):
		result = domain.TaskCancelled
	case err != nil:
		result = domain.TaskFailed
	}
	log.InfoContext(ctx, "task finished", slog.String("result", string(result)), slog.Any("err", err))
	// The run's own context may be cancelled; its outcome is still recorded.
	if err := s.store.TaskFinished(context.WithoutCancel(ctx), task.Key, time.Now(), result, err); err != nil {
		log.WarnContext(ctx, "task outcome not recorded", slog.Any("err", err))
	}
}
