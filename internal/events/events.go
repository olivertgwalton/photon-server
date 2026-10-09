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
	// changeWindow is how long a library's changed titles are gathered before they are told, so a
	// scan of hundreds of files is a handful of events, as Jellyfin gathers its LibraryChanged.
	changeWindow = 3 * time.Second
	// scanLife is how long a scan's progress is kept unless it is told again, so a node that dies
	// mid-scan leaves none behind.
	scanLife = 2 * time.Minute
	// backlogProgressEvery is the most often a kind's backlog tells how far it has got, besides
	// once when none is left.
	backlogProgressEvery = time.Second
	// backlogLife is how long a kind's count of jobs done outlives its last, so a backlog emptied
	// without its jobs ending, as cancelled conversions are, is not counted into the next.
	backlogLife = 24 * time.Hour
)

// Server is the server a webhook is told an event happened on.
type Server struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

type Hub struct {
	store *store.Store
	kv    *kv.KV
	// server is the server's id, and name what it is called when a webhook is told.
	server uuid.UUID
	name   func() string
	log    *slog.Logger

	mu      sync.Mutex
	streams map[chan domain.Event]struct{}
	closed  bool
	// changed is each library's titles changed and not yet told.
	changed map[uuid.UUID]store.Changed
	// toldBacklog is when each kind's backlog was last told from this node.
	toldBacklog map[domain.JobKind]time.Time
}

func New(st *store.Store, k *kv.KV, server uuid.UUID, name func() string, log *slog.Logger) *Hub {
	return &Hub{
		store: st, kv: k, server: server, name: name, log: log,
		streams: map[chan domain.Event]struct{}{}, changed: map[uuid.UUID]store.Changed{},
		toldBacklog: map[domain.JobKind]time.Time{},
	}
}

// Raise keeps an event in the activity log if it is a kind the log keeps, queues it for the
// webhooks that asked for it, and tells every node's streams. It never fails what raised it: what
// goes wrong is logged, and a webhook is called by a job, never here.
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
	if e.Kind.Hookable() {
		err := h.store.QueueWebhooks(ctx, e.Kind, func(ctx context.Context) ([]byte, error) { return h.payload(ctx, e) })
		if err != nil {
			h.log.WarnContext(ctx, "webhooks not queued", slog.String("kind", string(e.Kind)), slog.Any("err", err))
		}
	}
	message, err := json.Marshal(e)
	if err == nil {
		err = h.kv.PublishEvent(ctx, string(message))
	}
	if err != nil {
		h.log.WarnContext(ctx, "event not told", slog.String("kind", string(e.Kind)), slog.Any("err", err))
	}
}

// TestWebhook queues a test event to one webhook. store.ErrNotFound for no such webhook.
func (h *Hub) TestWebhook(ctx context.Context, id uuid.UUID) error {
	e := domain.Event{Kind: domain.EventWebhookTest, At: time.Now()}
	body, err := h.payload(ctx, e)
	if err != nil {
		return err
	}
	return h.store.QueueDelivery(ctx, id, e.Kind, body)
}

type named struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

type title struct {
	ID    uuid.UUID       `json:"id"`
	Kind  domain.ItemKind `json:"kind"`
	Title string          `json:"title"`
	Year  *int            `json:"year,omitzero"`
	// IDs are the title's on IMDb, TMDB and TheTVDB, as a scrobbler finds it by.
	IDs     map[domain.Provider]string `json:"ids,omitempty"`
	Season  *int                       `json:"season,omitzero"`
	Episode *int                       `json:"episode,omitzero"`
	Show    *title                     `json:"show,omitzero"`
}

// payload is the body a webhook is sent, as Plex's carries its server, account and metadata: the
// event, the server, the profile, title and library it is about where it is about one still
// there, and its details.
func (h *Hub) payload(ctx context.Context, e domain.Event) ([]byte, error) {
	d, err := h.store.Describe(ctx, e)
	if err != nil {
		return nil, err
	}
	body := struct {
		Event   domain.EventKind    `json:"event"`
		At      time.Time           `json:"at"`
		Server  Server              `json:"server"`
		Profile *named              `json:"profile,omitzero"`
		Title   *title              `json:"title,omitzero"`
		Library *named              `json:"library,omitzero"`
		Details domain.EventDetails `json:"details"`
	}{Event: e.Kind, At: e.At.UTC(), Server: Server{ID: h.server, Name: h.name()}, Details: e.Details}
	if body.Details == nil {
		body.Details = domain.NoDetails{}
	}
	if d.ProfileName != nil {
		body.Profile = &named{ID: e.Profile, Name: *d.ProfileName}
	}
	if d.Title != nil {
		body.Title = &title{ID: e.Item, Kind: *d.TitleKind, Title: *d.Title, Year: d.Year, IDs: d.TitleIDs, Season: d.Season, Episode: d.Episode}
		if d.Show != nil {
			body.Title.Show = &title{ID: *d.Show, Kind: domain.ItemShow, Title: *d.ShowTitle, Year: d.ShowYear, IDs: d.ShowIDs}
		}
	}
	if d.LibraryName != nil {
		body.Library = &named{ID: e.Library, Name: *d.LibraryName}
	}
	return json.Marshal(body)
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
		h.Raise(ctx, domain.Event{Kind: domain.EventScanProgress, Library: p.Library, Details: domain.ScanProgressDetails{
			Phase: p.Phase, Done: p.Done, Known: p.Known, Folder: p.Folder,
		}})
	}
}

// Backlogs answers the backlog of each kind of job with any left, across the cluster.
func (h *Hub) Backlogs(ctx context.Context) ([]domain.Backlog, error) {
	left, err := h.store.JobsLeft(ctx)
	if err != nil {
		return nil, err
	}
	done, err := h.kv.JobsDone(ctx)
	if err != nil {
		return nil, err
	}
	var out []domain.Backlog
	for _, kind := range domain.JobKinds() {
		if left[kind] > 0 {
			out = append(out, domain.Backlog{Kind: kind, Left: left[kind], Done: done[kind]})
		}
	}
	return out, nil
}

// JobEnded counts a job of kind that finished or gave up as done in its backlog, and tells how far
// the backlog has got: at most every backlogProgressEvery, and always once none is left, when its
// count starts again.
func (h *Hub) JobEnded(ctx context.Context, kind domain.JobKind) {
	ctx = context.WithoutCancel(ctx)
	done, err := h.kv.JobDone(ctx, kind, backlogLife)
	if err != nil {
		h.log.WarnContext(ctx, "job not counted", slog.String("kind", string(kind)), slog.Any("err", err))
		return
	}
	// Counting the backlog reads every job left, so it waits until a report is due; whether any is
	// left at all is answered from the first one found.
	anyLeft, err := h.store.AnyJobsLeft(ctx, kind)
	if err != nil {
		h.log.WarnContext(ctx, "jobs left not counted", slog.Any("err", err))
		return
	}
	var left int
	if anyLeft {
		if !h.backlogDue(kind) {
			return
		}
		all, err := h.store.JobsLeft(ctx)
		if err != nil {
			h.log.WarnContext(ctx, "jobs left not counted", slog.Any("err", err))
			return
		}
		left = all[kind]
	}
	if left == 0 {
		if err := h.kv.EndBacklog(ctx, kind); err != nil {
			h.log.WarnContext(ctx, "backlog not ended", slog.String("kind", string(kind)), slog.Any("err", err))
		}
	}
	h.Raise(ctx, domain.Event{Kind: domain.EventJobsProgress, Details: domain.BacklogDetails{
		JobKind: kind, Left: left, Done: done,
	}})
}

// BacklogStopped starts kind's count of jobs done again, its jobs taken off the queue, and tells
// that none is left.
func (h *Hub) BacklogStopped(ctx context.Context, kind domain.JobKind) error {
	if err := h.kv.EndBacklog(ctx, kind); err != nil {
		return err
	}
	h.Raise(ctx, domain.Event{Kind: domain.EventJobsProgress, Details: domain.BacklogDetails{JobKind: kind}})
	return nil
}

func (h *Hub) backlogDue(kind domain.JobKind) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	now := time.Now()
	if now.Sub(h.toldBacklog[kind]) < backlogProgressEvery {
		return false
	}
	h.toldBacklog[kind] = now
	return true
}

// Changed gathers a library's changed titles, telling them as one library.changed once
// changeWindow has passed since the first. It never waits on anything.
func (h *Hub) Changed(ctx context.Context, lib uuid.UUID, titles store.Changed) {
	h.mu.Lock()
	defer h.mu.Unlock()
	gathered, waiting := h.changed[lib]
	for change, ids := range titles {
		if len(ids) == 0 {
			continue
		}
		if !waiting {
			gathered, waiting = store.Changed{}, true
			h.changed[lib] = gathered
			time.AfterFunc(changeWindow, func() { h.tellChanged(context.WithoutCancel(ctx), lib) })
		}
		gathered[change] = append(gathered[change], ids...)
	}
}

func (h *Hub) tellChanged(ctx context.Context, lib uuid.UUID) {
	h.mu.Lock()
	gathered := h.changed[lib]
	delete(h.changed, lib)
	h.mu.Unlock()
	// A title is told once, under the change that says most of it.
	told := map[uuid.UUID]bool{}
	details := domain.LibraryChangedDetails{}
	for _, change := range domain.TitleChanges() {
		ids := []uuid.UUID{}
		for _, id := range gathered[change] {
			if !told[id] {
				told[id] = true
				ids = append(ids, id)
			}
		}
		details[change] = ids
	}
	h.Raise(ctx, domain.Event{Kind: domain.EventLibraryChanged, Library: lib, Details: details})
}

// Scanned forgets a scan's progress once it has ended.
func (h *Hub) Scanned(ctx context.Context, lib uuid.UUID) {
	if err := h.kv.EndScan(context.WithoutCancel(ctx), lib); err != nil {
		h.log.WarnContext(ctx, "scan progress not forgotten", slog.Any("err", err))
	}
}
