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
	// rereadEvery is how often a gate reads again what plays and the maintenance window, besides as
	// each playback starts or stops and as the window is changed: the window's edges, and an event
	// lost with Valkey's connection, are noticed within it.
	rereadEvery = time.Minute
	// resubscribeAfter is the wait before a gate follows events again once its stream has ended.
	resubscribeAfter = time.Second
)

var (
	// errPlayback stops a job holding a gate as a playback starts.
	errPlayback = fmt.Errorf("%w: a playback started", ErrNotNow)
	// errWindowClosed stops a job held to the maintenance window as it closes.
	errWindowClosed = fmt.Errorf("%w: the maintenance window closed", ErrNotNow)
)

type playbacks interface {
	Playbacks(ctx context.Context) ([]domain.Playback, error)
}

type maintenance interface {
	Maintenance(ctx context.Context) (domain.Maintenance, error)
}

// Gate keeps the jobs that read media out of playback's way. A chapter's still, a fingerprint or a
// walk for keyframes reads a file on the disk or network mount a stream reads from, and ten at once
// on a debrid mount left a stream nothing (measured: 64 MiB took minutes to read). So no such job
// starts while anything plays on any node of the cluster, and one running as a playback starts is
// stopped, to be queued again with its attempt given back, as a conversion gives way to a
// playback's transcode. Jellyfin's and Plex's background work pays playback no such regard.
//
// Work held to the maintenance window starts only inside it and is stopped as it closes, as
// Plex's butler is: all of a kind whose timing is the window, and of one also done as parts are
// added, what the window's backfill queued.
type Gate struct {
	playbacks playbacks
	settings  maintenance
	subscribe func() (<-chan domain.Event, func())
	log       *slog.Logger

	mu sync.Mutex
	// read is whether what plays and the window have been read; until they have, nothing starts.
	read    bool
	playing bool
	window  domain.Maintenance
	holds   map[*hold]struct{}
}

type hold struct {
	kind domain.JobKind
	due  domain.JobDue
	stop context.CancelCauseFunc
}

// NewGate reads what plays across the cluster from p and the maintenance window from settings,
// again as each event subscribe streams tells of a playback starting or stopping on any node, or
// of the window changed.
func NewGate(p playbacks, settings maintenance, subscribe func() (<-chan domain.Event, func()), log *slog.Logger) *Gate {
	return &Gate{playbacks: p, settings: settings, subscribe: subscribe, log: log, holds: map[*hold]struct{}{}}
}

// Run keeps the gate told what plays and when the window is until ctx ends.
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

// follow reads again as events tell of a playback starting or stopping or the window changed, and
// at every tick, until ctx ends or events does, as a stream that fell behind is ended.
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
			if e.Kind == domain.EventPlaybackStarted || e.Kind == domain.EventPlaybackStopped || e.Kind == domain.EventMaintenanceChanged {
				g.reread(ctx)
			}
		}
	}
}

// reread reads what plays and the window, and stops each job holding the gate that may not go on.
// What is not read leaves the gate as it was.
func (g *Gate) reread(ctx context.Context) {
	going, err := g.playbacks.Playbacks(ctx)
	var window domain.Maintenance
	if err == nil {
		window, err = g.settings.Maintenance(ctx)
	}
	if err != nil {
		if ctx.Err() == nil {
			g.log.WarnContext(ctx, "background work's gate not read", slog.Any("err", err))
		}
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.read, g.playing, g.window = true, len(going) > 0, window
	now := time.Now()
	for h := range g.holds {
		var cause error
		switch {
		case g.playing:
			cause = errPlayback
		case !g.inTime(h.kind, h.due, now):
			cause = errWindowClosed
		default:
			continue
		}
		delete(g.holds, h)
		h.stop(cause)
	}
}

// inTime reports whether work of kind, due as said, may run at now as its timing has it; the caller
// holds g.mu.
// Keyframes are what a play is cut at, read from a file's own index as it is added, as Plex
// analyses a file as it is added, so no window holds them.
func (g *Gate) inTime(kind domain.JobKind, due domain.JobDue, now time.Time) bool {
	var timing domain.Timing
	switch kind {
	case domain.JobPreviews:
		timing = g.window.Previews
	case domain.JobMarkers:
		timing = g.window.Markers
	case domain.JobKeyframes, domain.JobIdentify, domain.JobScanLibrary, domain.JobConvert, domain.JobDeliverWebhook, domain.JobTheme:
		return true
	}
	switch timing {
	case domain.TimingWindow:
		return g.window.Holds(now)
	case domain.TimingWindowAndAdded:
		switch due {
		case domain.JobDueNow:
		case domain.JobDueWindow:
			return g.window.Holds(now)
		}
	}
	return true
}

// Open reports whether a job of kind, due as said, may start now.
func (g *Gate) Open(kind domain.JobKind, due domain.JobDue) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.open(kind, due)
}

// open is Open; the caller holds g.mu.
func (g *Gate) open(kind domain.JobKind, due domain.JobDue) bool {
	return g.read && !g.playing && g.inTime(kind, due, time.Now())
}

// Hold lets a job of kind, due as said, run for as long as nothing plays and, for work held to the
// window, the window is open: ok is false where it may not start, and held is cancelled with a
// cause that is ErrNotNow when it may not go on. release gives the gate back once the job ends.
func (g *Gate) Hold(ctx context.Context, kind domain.JobKind, due domain.JobDue) (held context.Context, release func(), ok bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.open(kind, due) {
		return nil, nil, false
	}
	held, stop := context.WithCancelCause(ctx)
	h := &hold{kind: kind, due: due, stop: stop}
	g.holds[h] = struct{}{}
	return held, func() {
		g.mu.Lock()
		delete(g.holds, h)
		g.mu.Unlock()
		stop(nil)
	}, true
}

// Opens answers when past midnight the maintenance window opens, in its zone, or false until it
// has been read.
func (g *Gate) Opens() (at time.Duration, zone *time.Location, ok bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return time.Duration(g.window.StartHour) * time.Hour, g.window.Zone, g.read
}
