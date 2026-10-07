package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/follow"
)

const (
	// rereadEvery is how often a gate reads again what plays and the maintenance window, besides as
	// each playback starts or stops and as the window is changed: the window's edges, and an event
	// lost with Valkey's connection, are noticed within it.
	rereadEvery = time.Minute
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
// A job due in the maintenance window starts only inside it and is stopped as it closes, as Plex's
// butler is; one due now starts whenever nothing plays.
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
	// Read once subscribed, so no playback starts unseen between the two.
	follow.Events(ctx, g.subscribe, rereadEvery, g.reread,
		domain.EventPlaybackStarted, domain.EventPlaybackStopped, domain.EventMaintenanceChanged)
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
		case !g.inTime(h.due, now):
			cause = errWindowClosed
		default:
			continue
		}
		delete(g.holds, h)
		h.stop(cause)
	}
}

// inTime reports whether a job due as said may run at now; the caller holds g.mu.
func (g *Gate) inTime(due domain.JobDue, now time.Time) bool {
	switch due {
	case domain.JobDueNow:
		return true
	case domain.JobDueWindow:
		return g.window.Holds(now)
	}
	return false
}

// Open reports whether a job due as said may start now.
func (g *Gate) Open(due domain.JobDue) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.open(due)
}

// open is Open; the caller holds g.mu.
func (g *Gate) open(due domain.JobDue) bool {
	return g.read && !g.playing && g.inTime(due, time.Now())
}

// Hold lets a job due as said run for as long as nothing plays and, for one due in the window, the
// window is open: ok is false where it may not start, and held is cancelled with a cause that is
// ErrNotNow when it may not go on. release gives the gate back once the job ends.
func (g *Gate) Hold(ctx context.Context, due domain.JobDue) (held context.Context, release func(), ok bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.open(due) {
		return nil, nil, false
	}
	held, stop := context.WithCancelCause(ctx)
	h := &hold{due: due, stop: stop}
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

// When is a gate open while on says, which lets a job it held go on: as a node's role, which
// says whether it encodes video, opens it to downloads' conversions.
type When func() bool

func (w When) Open(domain.JobDue) bool { return w() }

func (w When) Hold(ctx context.Context, _ domain.JobDue) (context.Context, func(), bool) {
	return ctx, func() {}, w()
}
