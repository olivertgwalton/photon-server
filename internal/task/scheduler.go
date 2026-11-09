package task

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"sync/atomic"
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
	TaskFinished(ctx context.Context, key domain.TaskKey, started, at time.Time, result domain.TaskResult, err error) error
	TasksAbandoned(ctx context.Context, at time.Time) error
}

// Scheduler runs due tasks on whichever node holds the scheduler lease. Each task runs alone, and
// losing the lease cancels whatever is running so two nodes never run one task at once.
type Scheduler struct {
	store stateStore
	log   *slog.Logger
	node  uuid.UUID
	raise func(context.Context, domain.Event)
	tasks []Task
	// leads is whether this node held the lease when it last asked.
	leads atomic.Bool
	// renewed is when the lease was last known held, for Run alone to read and set.
	renewed time.Time
}

// node identifies this process to the other nodes of the cluster; raise says as each task starts
// and ends.
func NewScheduler(st stateStore, log *slog.Logger, node uuid.UUID, raise func(context.Context, domain.Event), tasks ...Task) *Scheduler {
	return &Scheduler{store: st, log: log, node: node, raise: raise, tasks: tasks}
}

// Leads reports whether this node holds the scheduler lease: one node of the cluster at a time.
func (s *Scheduler) Leads() bool { return s.leads.Load() }

func (s *Scheduler) Run(ctx context.Context) {
	t := time.NewTicker(tick)
	defer t.Stop()
	// A node draining goes on serving after it stops asking, while another takes the lease.
	defer s.leads.Store(false)
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
	// Whatever the last leader left running ended with it.
	if err := s.store.TasksAbandoned(ctx, time.Now()); err != nil {
		s.log.WarnContext(ctx, "abandoned tasks not ended", slog.Any("err", err))
	}
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
				s.run(ctx, task, start, now)
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
	asked := time.Now()
	held, err := s.store.HoldLease(ctx, leaseName, s.node, leaseTTL)
	switch {
	case err != nil:
		if ctx.Err() == nil {
			s.log.WarnContext(ctx, "scheduler lease not renewed", slog.Any("err", err))
		}
		// No answer says nothing of the lease, and none can take it until it has run out.
		held = time.Since(s.renewed) < leaseTTL
	case held:
		s.renewed = asked
	default:
		s.renewed = time.Time{}
	}
	s.leads.Store(held)
	return held
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

// perform runs task, a panic in it failing the run as an error does rather than ending the node.
func perform(ctx context.Context, log *slog.Logger, task Task, start Start) (err error) {
	defer func() {
		if p := recover(); p != nil {
			log.ErrorContext(ctx, "task panicked", slog.Any("panic", p), slog.String("stack", string(debug.Stack())))
			err = fmt.Errorf("panic: %v", p)
		}
	}()
	return task.Run(ctx, start)
}

func (s *Scheduler) run(ctx context.Context, task Task, start Start, started time.Time) {
	log := s.log.With(slog.String("task", string(task.Key)))
	log.InfoContext(ctx, "task started")
	s.raise(ctx, domain.Event{Kind: domain.EventTaskStarted, Details: domain.TaskDetails{Task: task.Key}})
	err := perform(ctx, log, task, start)
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
	if err := s.store.TaskFinished(ctx, task.Key, started, time.Now(), result, err); err != nil {
		log.WarnContext(ctx, "task outcome not recorded", slog.Any("err", err))
	}
	d := domain.TaskDetails{Task: task.Key, Result: result}
	if err != nil {
		d.Error = err.Error()
	}
	s.raise(ctx, domain.Event{Kind: kind, Details: d})
}
