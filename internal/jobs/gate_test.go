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

// cluster is what plays on every node, as Valkey keeps it, and the events every node is told.
type cluster struct {
	mu      sync.Mutex
	playing []domain.Playback
	events  chan domain.Event
}

func newCluster() *cluster { return &cluster{events: make(chan domain.Event, 8)} }

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
	gate := NewGate(c, c.subscribe, slog.New(slog.DiscardHandler))
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
		q := &memoryQueue{pending: []domain.Job{{ID: 7, Kind: domain.JobPreviews}}}
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

// While anything plays on any node, no media work starts, and none is even claimed; once the last
// playback stops it starts.
func TestNoMediaWorkStartsWhileAnythingPlays(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		q := &memoryQueue{pending: []domain.Job{{ID: 7, Kind: domain.JobPreviews}}}
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
