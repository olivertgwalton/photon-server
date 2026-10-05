package task

import (
	"context"
	"log/slog"
	"maps"
	"sync"
	"testing"
	"testing/synctest"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

func TestTriggerDue(t *testing.T) {
	day := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	at := func(h, m int) time.Time { return day.Add(time.Duration(h)*time.Hour + time.Duration(m)*time.Minute) }
	every := Trigger{Kind: TriggerEvery, Every: 12 * time.Hour}
	nightly := Trigger{Kind: TriggerDaily, At: 3 * time.Hour}
	tests := []struct {
		name      string
		trigger   Trigger
		last, now time.Time
		want      bool
	}{
		{"never run is due at once", every, time.Time{}, at(9, 0), true},
		{"before the interval is up", every, at(1, 0), at(12, 59), false},
		{"once the interval is up", every, at(1, 0), at(13, 0), true},
		{"daily before today's time, ran yesterday after it", nightly, at(3, 5).AddDate(0, 0, -1), at(2, 0), false},
		{"daily after today's time", nightly, at(3, 5).AddDate(0, 0, -1), at(3, 0), true},
		{"daily already run today", nightly, at(3, 1), at(23, 0), false},
		{"daily missed while down for days", nightly, at(3, 0).AddDate(0, 0, -3), at(10, 0), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.trigger.due(tt.last, tt.now); got != tt.want {
				t.Errorf("due = %v, want %v", got, tt.want)
			}
		})
	}
}

type memoryStore struct {
	mu       sync.Mutex
	leader   bool
	starts   map[domain.TaskKey]time.Time
	outcomes []domain.TaskResult
}

func (m *memoryStore) HoldLease(context.Context, string, uuid.UUID, time.Duration) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.leader, nil
}

func (m *memoryStore) setLeader(held bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.leader = held
}

func (m *memoryStore) TaskStarts(context.Context) (map[domain.TaskKey]time.Time, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return maps.Clone(m.starts), nil
}

func (m *memoryStore) TaskStarted(_ context.Context, key domain.TaskKey, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.starts[key] = at
	return nil
}

func (m *memoryStore) TaskFinished(_ context.Context, _ domain.TaskKey, _ time.Time, result domain.TaskResult, _ error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.outcomes = append(m.outcomes, result)
	return nil
}

func (m *memoryStore) results() []domain.TaskResult {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]domain.TaskResult(nil), m.outcomes...)
}

func TestSchedulerRunsDueTasksOneAtATime(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		st := &memoryStore{leader: true, starts: map[domain.TaskKey]time.Time{}}
		var mu sync.Mutex
		runs, concurrent, most := 0, 0, 0
		task := Task{
			Key:      domain.TaskScanLibraries,
			Triggers: []Trigger{{Kind: TriggerEvery, Every: time.Hour}},
			Run: func(context.Context) error {
				mu.Lock()
				runs++
				concurrent++
				most = max(most, concurrent)
				mu.Unlock()
				<-time.After(90 * time.Minute) // longer than the interval
				mu.Lock()
				concurrent--
				mu.Unlock()
				return nil
			},
		}
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})
		go func() {
			NewScheduler(st, discard(), task).Run(ctx)
			close(done)
		}()

		synctest.Sleep(4 * time.Hour)
		cancel()
		<-done
		mu.Lock()
		defer mu.Unlock()
		if most != 1 {
			t.Errorf("%d runs overlapped, want none", most)
		}
		// Each run takes 90 minutes and is due again at once: starts at 0, 1:30, 3:00.
		if runs != 3 {
			t.Errorf("%d runs in four hours, want 3", runs)
		}
	})
}

func TestSchedulerRunsNothingWithoutTheLease(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		st := &memoryStore{starts: map[domain.TaskKey]time.Time{}}
		ran := false
		task := Task{
			Key:      domain.TaskScanLibraries,
			Triggers: []Trigger{{Kind: TriggerEvery, Every: time.Hour}},
			Run:      func(context.Context) error { ran = true; return nil },
		}
		ctx, cancel := context.WithCancel(t.Context())
		go NewScheduler(st, discard(), task).Run(ctx)
		synctest.Sleep(time.Hour)
		cancel()
		synctest.Wait()
		if ran {
			t.Error("a node without the lease ran a task")
		}
	})
}

func TestLosingTheLeaseCancelsTheRunningTask(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		st := &memoryStore{leader: true, starts: map[domain.TaskKey]time.Time{}}
		task := Task{
			Key:      domain.TaskScanLibraries,
			Triggers: []Trigger{{Kind: TriggerEvery, Every: 24 * time.Hour}},
			Run: func(ctx context.Context) error {
				<-ctx.Done()
				return ctx.Err()
			},
		}
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go NewScheduler(st, discard(), task).Run(ctx)
		synctest.Sleep(time.Minute)
		st.setLeader(false)
		synctest.Sleep(time.Minute)
		synctest.Wait()
		if got := st.results(); len(got) != 1 || got[0] != domain.TaskCancelled {
			t.Errorf("outcomes = %v, want one cancelled run", got)
		}
	})
}

func discard() *slog.Logger { return slog.New(slog.DiscardHandler) }
