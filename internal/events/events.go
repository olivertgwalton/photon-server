// Package events tells every node's admins what happens on any node, and keeps what they will
// read later in the activity log.
package events

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/kv"
	"github.com/olivertgwalton/photon-server/internal/store"
)

const (
	// streamBuffer is how far a stream may fall behind before it is ended, so a dashboard that
	// stops reading holds no one up; it reconnects to a fresh snapshot.
	streamBuffer = 256
	// resubscribeAfter is the wait before listening again once Valkey's connection is lost.
	resubscribeAfter = time.Second
	// scanProgressEvery is the most often a scan tells how far it has got, besides each change of
	// phase.
	scanProgressEvery = time.Second
	// scanLife is how long a scan's progress is kept unless it is told again, so a node that dies
	// mid-scan leaves none behind.
	scanLife = 2 * time.Minute
)

type Hub struct {
	store *store.Store
	kv    *kv.KV
	log   *slog.Logger

	mu      sync.Mutex
	streams map[chan domain.Event]struct{}
	closed  bool
}

func New(st *store.Store, k *kv.KV, log *slog.Logger) *Hub {
	return &Hub{store: st, kv: k, log: log, streams: map[chan domain.Event]struct{}{}}
}

// Raise keeps an event in the activity log if it is a kind the log keeps, and tells every node's
// streams. It never fails what raised it: what goes wrong is logged.
func (h *Hub) Raise(ctx context.Context, e domain.Event) {
	// What raised it may be ending, as a player that stops and goes.
	ctx = context.WithoutCancel(ctx)
	e.At = time.Now()
	if e.Kind.Logged() {
		id, err := h.store.AddActivity(ctx, e)
		if err != nil {
			h.log.WarnContext(ctx, "activity not kept", slog.String("kind", string(e.Kind)), slog.Any("err", err))
		}
		e.ID = id
	}
	message, err := json.Marshal(e)
	if err == nil {
		err = h.kv.PublishEvent(ctx, string(message))
	}
	if err != nil {
		h.log.WarnContext(ctx, "event not told", slog.String("kind", string(e.Kind)), slog.Any("err", err))
	}
}

// Run hands this node's streams every node's events until ctx ends, then ends them.
func (h *Hub) Run(ctx context.Context) {
	for {
		err := h.kv.ReceiveEvents(ctx, h.deliver)
		if ctx.Err() != nil {
			break
		}
		h.log.WarnContext(ctx, "events not received", slog.Any("err", err))
		select {
		case <-ctx.Done():
		case <-time.After(resubscribeAfter):
		}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	for s := range h.streams {
		close(s)
		delete(h.streams, s)
	}
}

func (h *Hub) deliver(message string) {
	var e domain.Event
	if err := json.Unmarshal([]byte(message), &e); err != nil {
		h.log.Warn("event not read", slog.Any("err", err))
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for s := range h.streams {
		select {
		case s <- e:
		default:
			close(s)
			delete(h.streams, s)
		}
	}
}

// Subscribe answers a stream of every event from now, and how to stop it. It is closed when the
// hub stops or the stream falls too far behind.
func (h *Hub) Subscribe() (<-chan domain.Event, func()) {
	s := make(chan domain.Event, streamBuffer)
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		close(s)
		return s, func() {}
	}
	h.streams[s] = struct{}{}
	return s, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if _, ok := h.streams[s]; ok {
			close(s)
			delete(h.streams, s)
		}
	}
}

// Scans answers every scan going on, across the cluster.
func (h *Hub) Scans(ctx context.Context) ([]domain.ScanProgress, error) {
	return h.kv.Scans(ctx)
}

// Scanning answers what one scan tells its progress to: kept for the snapshot while it lasts,
// and told to streams.
func (h *Hub) Scanning(ctx context.Context) func(domain.ScanProgress) {
	var last time.Time
	var phase domain.ScanPhase
	return func(p domain.ScanProgress) {
		if now := time.Now(); p.Phase != phase || now.Sub(last) >= scanProgressEvery {
			last, phase = now, p.Phase
		} else {
			return
		}
		if err := h.kv.SaveScan(ctx, p, scanLife); err != nil {
			h.log.WarnContext(ctx, "scan progress not kept", slog.Any("err", err))
		}
		h.Raise(ctx, domain.Event{Kind: domain.EventScanProgress, Library: p.Library, Details: map[string]any{
			"phase": p.Phase, "done": p.Done, "known": p.Known,
		}})
	}
}

// Scanned forgets a scan's progress once it has ended.
func (h *Hub) Scanned(ctx context.Context, lib uuid.UUID) {
	if err := h.kv.EndScan(context.WithoutCancel(ctx), lib); err != nil {
		h.log.WarnContext(ctx, "scan progress not forgotten", slog.Any("err", err))
	}
}
