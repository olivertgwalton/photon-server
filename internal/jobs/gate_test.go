package jobs

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"testing/synctest"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// cluster is what plays on every node, as Valkey keeps it, the events every node is told, and the
// maintenance window, as Postgres keeps it.
type cluster struct {
	mu      sync.Mutex
	playing []domain.Playback
	events  chan domain.Event
	window  domain.Maintenance
}

// newCluster keeps the window from 02:00 to 05:00; a test starts at midnight.
func newCluster() *cluster {
	w := domain.Maintenance{StartHour: 2, EndHour: 5, Zone: time.UTC, Previews: domain.TimingWindow, Markers: domain.TimingWindow}
	return &cluster{events: make(chan domain.Event, 8), window: w}
}

func (c *cluster) Maintenance(context.Context) (domain.Maintenance, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.window, nil
}

// change sets the window anew, telling every node.
func (c *cluster) change(start, end int) {
	c.mu.Lock()
	c.window.StartHour, c.window.EndHour = start, end
	c.mu.Unlock()
	c.events <- domain.Event{Kind: domain.EventMaintenanceChanged}
}

func (c *cluster) Playbacks(context.Context) ([]domain.Playback, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.playing, nil
}

func (c *cluster) subscribe() (<-chan domain.Event, func()) { return c.events, func() {} }

// play starts a playback on node, or stops every one where node is nil, telling every node.
func (c *cluster) play(node *uuid.UUID) {
	c.mu.Lock()
	kind := domain.EventPlaybackStopped
	c.playing = nil
	if node != nil {
		kind = domain.EventPlaybackStarted
		c.playing = []domain.Playback{{ID: uuid.NewV7(), Node: *node, State: domain.StatePlaying}}
	}
	c.mu.Unlock()
	c.events <- domain.Event{Kind: kind}
}

func runReader(t *testing.T, q *memoryQueue, c *cluster, h Handler) func() {
	t.Helper()
	gate := NewGate(c, c, c.subscribe, slog.New(slog.DiscardHandler))
	w := NewWorker(q, slog.New(slog.DiscardHandler), uuid.NewV7(), 1, map[domain.JobKind]Handler{domain.JobPreviews: h}, ignore{}, gate)
	ctx, cancel := context.WithCancel(t.Context())
	var wg sync.WaitGroup
	wg.Go(func() { gate.Run(ctx) })
	wg.Go(func() { w.Run(ctx) })
	return func() {
		cancel()
		wg.Wait()
	}
}

// A preview being made when someone presses play on another node stops at once, and is queued to
// be made later without spending one of its attempts.
func TestAPlaybackStopsMediaWorkAndGivesItsAttemptBack(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		q := &memoryQueue{pending: []domain.Job{{ID: 7, Kind: domain.JobPreviews, Due: domain.JobDueNow}}}
		c := newCluster()
		started, stopped := make(chan struct{}), make(chan error, 1)
		stop := runReader(t, q, c, func(ctx context.Context, _ uuid.UUID) error {
			close(started)
			<-ctx.Done()
			stopped <- context.Cause(ctx)
			return ctx.Err()
		})
		defer stop()
		<-started
		other := uuid.NewV7()
		c.play(&other)
		if err := <-stopped; !errors.Is(err, ErrNotNow) {
			t.Fatalf("job stopped for %v, want a playback starting", err)
		}
		synctest.Wait()
		if len(q.postponed) != 1 || q.postponed[0] != 7 || len(q.failed)+len(q.completed) != 0 {
			t.Errorf("postponed %v, failed %v, completed %v; want job 7 queued again", q.postponed, q.failed, q.completed)
		}
	})
}

// While anything plays on any node, no media work starts, not even what an admin asked for now, and
// none is even claimed; once the last playback stops it starts.
func TestNoMediaWorkStartsWhileAnythingPlays(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		q := &memoryQueue{pending: []domain.Job{{ID: 7, Kind: domain.JobPreviews, Due: domain.JobDueNow}}}
		c := newCluster()
		other := uuid.NewV7()
		c.playing = []domain.Playback{{ID: uuid.NewV7(), Node: other}}
		ran := 0
		stop := runReader(t, q, c, func(context.Context, uuid.UUID) error {
			ran++
			return nil
		})
		defer stop()
		synctest.Sleep(time.Hour)
		if ran != 0 || len(q.pending) != 1 {
			t.Fatalf("ran %d, %d left queued while playing; want nothing claimed", ran, len(q.pending))
		}
		c.play(nil)
		synctest.Sleep(time.Minute)
		if ran != 1 || len(q.completed) != 1 {
			t.Errorf("ran %d, completed %v once playback stopped; want job 7 made", ran, q.completed)
		}
	})
}

// Previews due in the window, as the window's own backfill queues them, wait for it to open, and
// one still being made as it closes stops, to be made in the next, without spending one of its
// attempts.
func TestWindowedWorkStartsInTheWindowAndStopsAsItCloses(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		q := &memoryQueue{pending: []domain.Job{{ID: 7, Kind: domain.JobPreviews, Due: domain.JobDueWindow}}}
		c := newCluster()
		var mu sync.Mutex
		var startedAt time.Time
		var cause error
		stop := runReader(t, q, c, func(ctx context.Context, _ uuid.UUID) error {
			mu.Lock()
			startedAt = time.Now()
			mu.Unlock()
			<-ctx.Done()
			mu.Lock()
			cause = context.Cause(ctx)
			mu.Unlock()
			return ctx.Err()
		})
		defer stop()
		opens := time.Now().Add(2 * time.Hour)
		synctest.Sleep(6 * time.Hour)
		mu.Lock()
		defer mu.Unlock()
		if startedAt.Before(opens) || startedAt.After(opens.Add(2*time.Minute)) {
			t.Errorf("started at %s, want as the window opens at %s", startedAt, opens)
		}
		if !errors.Is(cause, errWindowClosed) || len(q.postponed) != 1 || len(q.failed) != 0 {
			t.Errorf("stopped for %v, postponed %v, failed %v; want stopped as the window closed and queued again",
				cause, q.postponed, q.failed)
		}
	})
}

// A job due now, as an admin's Run now makes a task's backlog, starts at midnight and runs to its
// end through the window's close, as Jellyfin's manual run carries no time limit.
func TestWorkDueNowRunsOutsideTheWindowToItsEnd(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		q := &memoryQueue{pending: []domain.Job{{ID: 7, Kind: domain.JobPreviews, Due: domain.JobDueNow}}}
		c := newCluster()
		var startedAt time.Time
		stop := runReader(t, q, c, func(ctx context.Context, _ uuid.UUID) error {
			startedAt = time.Now()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(6 * time.Hour):
				return nil
			}
		})
		defer stop()
		synctest.Sleep(7 * time.Hour)
		if startedAt.Hour() != 0 || len(q.completed) != 1 || len(q.postponed) != 0 {
			t.Errorf("started at %s, completed %v, postponed %v; want started at once and run past 05:00 to its end",
				startedAt, q.completed, q.postponed)
		}
	})
}

// An admin moving the window off the present hour stops work held to it on every node at once, not
// at the next minute.
func TestAChangedWindowIsKeptToAtOnce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		q := &memoryQueue{pending: []domain.Job{{ID: 7, Kind: domain.JobPreviews, Due: domain.JobDueWindow}}}
		c := newCluster()
		c.window.StartHour = 0
		started, stopped := make(chan struct{}), make(chan error, 1)
		stop := runReader(t, q, c, func(ctx context.Context, _ uuid.UUID) error {
			close(started)
			<-ctx.Done()
			stopped <- context.Cause(ctx)
			return ctx.Err()
		})
		defer stop()
		<-started
		changed := time.Now()
		c.change(2, 5)
		if err := <-stopped; !errors.Is(err, errWindowClosed) || time.Since(changed) > 0 {
			t.Errorf("stopped for %v after %s; want stopped as the window changed", err, time.Since(changed))
		}
	})
}
