package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

const (
	// rereadEvery is how often a gate reads again what plays, besides as each playback starts or
	// stops: an event lost with Valkey's connection is made good within it.
	rereadEvery = time.Minute
	// resubscribeAfter is the wait before a gate follows events again once its stream has ended.
	resubscribeAfter = time.Second
)

// errPlayback stops a job holding a gate as a playback starts.
var errPlayback = fmt.Errorf("%w: a playback started", ErrNotNow)

type playbacks interface {
	Playbacks(ctx context.Context) ([]domain.Playback, error)
}

// Gate keeps the jobs that read media out of playback's way. A chapter's still, a fingerprint or a
// walk for keyframes reads a file on the disk or network mount a stream reads from, and ten at once
// on a debrid mount left a stream nothing (measured: 64 MiB took minutes to read). So no such job
// starts while anything plays on any node of the cluster, and one running as a playback starts is
// stopped, to be queued again with its attempt given back, as a conversion gives way to a
// playback's transcode. Jellyfin's and Plex's background work pays playback no such regard.
type Gate struct {
	playbacks playbacks
	subscribe func() (<-chan domain.Event, func())
	log       *slog.Logger

	mu sync.Mutex
	// read is whether what plays has been read; until it has, nothing starts.
	read    bool
	playing bool
	holds   map[*hold]struct{}
}

type hold struct {
	stop context.CancelCauseFunc
}

// NewGate reads what plays across the cluster from p, again as each event subscribe streams tells
// of a playback starting or stopping on any node.
func NewGate(p playbacks, subscribe func() (<-chan domain.Event, func()), log *slog.Logger) *Gate {
	return &Gate{playbacks: p, subscribe: subscribe, log: log, holds: map[*hold]struct{}{}}
}

// Run keeps the gate told what plays until ctx ends.
func (g *Gate) Run(ctx context.Context) {
	t := time.NewTicker(rereadEvery)
	defer t.Stop()
	for ctx.Err() == nil {
		events, stop := g.subscribe()
		// Read once subscribed, so no playback starts unseen between the two.
		g.reread(ctx)
		g.follow(ctx, events, t.C)
		stop()
	}
}

// follow reads again as events tell of a playback starting or stopping, and at every tick, until ctx
// ends or events does, as a stream that fell behind is ended.
func (g *Gate) follow(ctx context.Context, events <-chan domain.Event, tick <-chan time.Time) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick:
			g.reread(ctx)
		case e, ok := <-events:
			if !ok {
				select {
				case <-ctx.Done():
				case <-time.After(resubscribeAfter):
				}
				return
			}
			if e.Kind == domain.EventPlaybackStarted || e.Kind == domain.EventPlaybackStopped {
				g.reread(ctx)
			}
		}
	}
}

// reread reads what plays, and stops every job holding the gate if anything does. What is not read
// leaves the gate as it was.
func (g *Gate) reread(ctx context.Context) {
	going, err := g.playbacks.Playbacks(ctx)
	if err != nil {
		if ctx.Err() == nil {
			g.log.WarnContext(ctx, "playbacks not read for background work", slog.Any("err", err))
		}
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.read, g.playing = true, len(going) > 0
	if !g.playing {
		return
	}
	for h := range g.holds {
		delete(g.holds, h)
		h.stop(errPlayback)
	}
}

// Open reports whether a job may start now.
func (g *Gate) Open() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.open()
}

// open is Open; the caller holds g.mu.
func (g *Gate) open() bool { return g.read && !g.playing }

// Hold lets a job run for as long as nothing plays: ok is false where something does, and held is
// cancelled with a cause that is ErrNotNow when a playback starts. release gives the gate back
// once the job ends.
func (g *Gate) Hold(ctx context.Context) (held context.Context, release func(), ok bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.open() {
		return nil, nil, false
	}
	held, stop := context.WithCancelCause(ctx)
	h := &hold{stop: stop}
	g.holds[h] = struct{}{}
	return held, func() {
		g.mu.Lock()
		delete(g.holds, h)
		g.mu.Unlock()
		stop(nil)
	}, true
}
