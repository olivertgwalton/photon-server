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

// Start is how a task's run began: by its trigger, or by an admin asking for it. As Jellyfin's
// manual run carries none of its trigger's limits, work a run queues may be held to the
// maintenance window by its trigger and never by an admin's asking.
type Start string

const (
	StartTrigger Start = "trigger"
	StartRequest Start = "request"
)

type Task struct {
	Key     domain.TaskKey
	Trigger Trigger
	Run     func(ctx context.Context, start Start) error
}

type stateStore interface {
	HoldLease(ctx context.Context, name string, node uuid.UUID, ttl time.Duration) (bool, error)
	TaskStates(ctx context.Context) (map[domain.TaskKey]domain.TaskState, error)
	RequestTask(ctx context.Context, key domain.TaskKey) error
	TaskStarted(ctx context.Context, key domain.TaskKey, at time.Time) error
	TaskFinished(ctx context.Context, key domain.TaskKey, at time.Time, result domain.TaskResult, err error) error
}

// Scheduler runs due tasks on whichever node holds the scheduler lease. Each task runs alone, and
// losing the lease cancels whatever is running so two nodes never run one task at once.
type Scheduler struct {
	store stateStore
	log   *slog.Logger
	node  uuid.UUID
	raise func(context.Context, domain.Event)
	tasks []Task
}

// node identifies this process to the other nodes of the cluster; raise says as each task starts
// and ends.
func NewScheduler(st stateStore, log *slog.Logger, node uuid.UUID, raise func(context.Context, domain.Event), tasks ...Task) *Scheduler {
	return &Scheduler{store: st, log: log, node: node, raise: raise, tasks: tasks}
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
		states, err := s.store.TaskStates(ctx)
		if err != nil {
			s.log.WarnContext(ctx, "task state not read", slog.Any("err", err))
		}
		now := time.Now()
		for _, task := range s.tasks {
			mu.Lock()
			busy := running[task.Key]
			mu.Unlock()
			if err != nil || busy || !isDue(task, states[task.Key], now) {
				continue
			}
			start := StartTrigger
			if st := states[task.Key]; st.Requested.After(st.Started) {
				start = StartRequest
			}
			if err := s.store.TaskStarted(ctx, task.Key, now); err != nil {
				s.log.WarnContext(ctx, "task not started", slog.String("task", string(task.Key)), slog.Any("err", err))
				continue
			}
			mu.Lock()
			running[task.Key] = true
			mu.Unlock()
			wg.Go(func() {
				s.run(ctx, task, start)
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

// isDue reports whether a task was asked for since it last started, or its trigger has fired.
func isDue(task Task, state domain.TaskState, now time.Time) bool {
	return state.Requested.After(state.Started) || task.Trigger.due(state.Started, now)
}

// ErrNoTask is a task the scheduler does not run.
var ErrNoTask = errors.New("task: no such task")

// Status is a task as an admin sees it: its last run, whether it is running, and when it next
// will.
type Status struct {
	Key     domain.TaskKey
	State   domain.TaskState
	Running bool
	Next    time.Time
}

// Statuses answers every task's status, in the order the scheduler was given them.
func (s *Scheduler) Statuses(ctx context.Context) ([]Status, error) {
	states, err := s.store.TaskStates(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	out := make([]Status, len(s.tasks))
	for i, task := range s.tasks {
		st := states[task.Key]
		next := now
		if !isDue(task, st, now) {
			next = task.Trigger.next(st.Started, now)
		}
		out[i] = Status{Key: task.Key, State: st, Running: !st.Started.IsZero() && st.Started.After(st.Finished), Next: next}
	}
	return out, nil
}

// Request asks for a task to run as soon as it is not running, on whichever node leads.
func (s *Scheduler) Request(ctx context.Context, key domain.TaskKey) error {
	for _, task := range s.tasks {
		if task.Key == key {
			return s.store.RequestTask(ctx, key)
		}
	}
	return ErrNoTask
}

func (s *Scheduler) run(ctx context.Context, task Task, start Start) {
	log := s.log.With(slog.String("task", string(task.Key)))
	log.InfoContext(ctx, "task started")
	s.raise(ctx, domain.Event{Kind: domain.EventTaskStarted, Details: map[string]any{"task": task.Key}})
	err := task.Run(ctx, start)
	result, kind := domain.TaskSucceeded, domain.EventTaskFinished
	switch {
	case errors.Is(err, context.Canceled):
		result = domain.TaskCancelled
	case err != nil:
		result, kind = domain.TaskFailed, domain.EventTaskFailed
	}
	log.InfoContext(ctx, "task finished", slog.String("result", string(result)), slog.Any("err", err))
	// The run's own context may be cancelled; its outcome is still recorded.
	ctx = context.WithoutCancel(ctx)
	if err := s.store.TaskFinished(ctx, task.Key, time.Now(), result, err); err != nil {
		log.WarnContext(ctx, "task outcome not recorded", slog.Any("err", err))
	}
	finished := domain.Event{Kind: kind, Details: map[string]any{"task": task.Key, "result": result}}
	if err != nil {
		finished.Details["error"] = err.Error()
	}
	s.raise(ctx, finished)
}
