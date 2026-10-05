package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/playback"
)

// heartbeatEvery is how often a quiet event stream sends a comment, so proxies that close idle
// connections, after a minute in nginx's default, leave it open.
const heartbeatEvery = 15 * time.Second

// sendWithin is how long one event may take to reach a client before its stream is given up, so a
// client that stops reading but keeps its connection does not hold the stream for good.
const sendWithin = 10 * time.Second

type activityLog interface {
	Activity(ctx context.Context, kind domain.EventKind, offset, limit int) ([]domain.Event, int64, error)
}

type eventHub interface {
	Raise(ctx context.Context, e domain.Event)
	Subscribe() (<-chan domain.Event, func())
	Scans(ctx context.Context) ([]domain.ScanProgress, error)
	TestWebhook(ctx context.Context, id uuid.UUID) error
}

// eventJSON is an event as the log lists it and the stream tells it; id is its activity entry's.
type eventJSON struct {
	ID        uuid.UUID        `json:"id,omitzero"`
	Kind      domain.EventKind `json:"kind"`
	At        time.Time        `json:"at"`
	ProfileID uuid.UUID        `json:"profile_id,omitzero"`
	TitleID   uuid.UUID        `json:"title_id,omitzero"`
	LibraryID uuid.UUID        `json:"library_id,omitzero"`
	Details   map[string]any   `json:"details"`
}

func eventOf(e domain.Event) eventJSON {
	return eventJSON{
		ID: e.ID, Kind: e.Kind, At: e.At.UTC(), ProfileID: e.Profile, TitleID: e.Item, LibraryID: e.Library,
		Details: nonNilMap(e.Details),
	}
}

func nonNilMap(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

// adminActivity answers a page of the activity log, the newest first, as Jellyfin's dashboard
// lists it: every kind, or one.
func (a *API) adminActivity(w http.ResponseWriter, r *http.Request) {
	kind := domain.EventKind(r.URL.Query().Get("kind"))
	if kind != "" && !slices.Contains(domain.LoggedEventKinds(), kind) {
		writeProblem(w, a.logger, codeInvalidParameter, fmt.Sprintf("kind is one of %v", domain.LoggedEventKinds()))
		return
	}
	offset, limit, ok := a.paging(w, r)
	if !ok {
		return
	}
	entries, total, err := a.svc.Activity.Activity(r.Context(), kind, offset, limit)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	out := make([]eventJSON, len(entries))
	for i, e := range entries {
		out[i] = eventOf(e)
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, pageJSON[eventJSON]{out, offset, total})
}

// adminEvents streams what happens on every node as Server-Sent Events, as Plex's notification
// stream does: first a snapshot of what is going on now, so a dashboard needs no second request,
// then each event as it happens, named by its kind.
func (a *API) adminEvents(w http.ResponseWriter, r *http.Request) {
	events, stop := a.svc.Events.Subscribe()
	defer stop()
	// Subscribed first, so nothing between the snapshot and the stream is missed; what arrives
	// twice is a state the dashboard already shows.
	now, err := a.snapshot(r.Context())
	if err != nil {
		a.internal(w, r, err)
		return
	}
	a.streamEvents(w, r, events, "snapshot", now, func(e domain.Event) (eventJSON, bool, error) {
		return eventOf(e), true, nil
	})
}

// streamEvents sends first, named name, then what tell makes of each event it says to send, named
// by its kind, with a comment when quiet, until the client goes or the events end.
func (a *API) streamEvents(w http.ResponseWriter, r *http.Request, events <-chan domain.Event, name string, first any, tell func(domain.Event) (eventJSON, bool, error)) {
	rc := http.NewResponseController(w)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	// nginx buffers a response unless told not to.
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	if a.sendEvent(w, rc, name, first) != nil {
		return
	}
	heartbeat := time.NewTicker(heartbeatEvery)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case e, open := <-events:
			if !open {
				return
			}
			told, send, err := tell(e)
			if err != nil {
				// The client reconnects and asks again for what it shows.
				a.logger.WarnContext(r.Context(), "event stream ended", slog.Any("err", err))
				return
			}
			if send && a.sendEvent(w, rc, string(e.Kind), told) != nil {
				return
			}
		case <-heartbeat.C:
			if sendText(w, rc, ": heartbeat\n\n") != nil {
				return
			}
		}
	}
}

func (a *API) sendEvent(w http.ResponseWriter, rc *http.ResponseController, name string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return sendText(w, rc, fmt.Sprintf("event: %s\ndata: %s\n\n", name, data))
}

func sendText(w http.ResponseWriter, rc *http.ResponseController, text string) error {
	if err := rc.SetWriteDeadline(time.Now().Add(sendWithin)); err != nil {
		return err
	}
	if _, err := io.WriteString(w, text); err != nil {
		return err
	}
	return rc.Flush()
}

// eventStream is what adminEvents sends: the snapshot, then events named by their kinds.
func eventStream() asStream {
	out := asStream{"snapshot": snapshotJSON{}}
	for _, k := range domain.EventKinds() {
		out[string(k)] = eventJSON{}
	}
	return out
}

type snapshotJSON struct {
	Tasks     []runningTaskJSON     `json:"tasks"`
	Jobs      []runningJobJSON      `json:"jobs"`
	Scans     []scanJSON            `json:"scans"`
	Playbacks []playback.NowPlaying `json:"playbacks"`
}

type runningTaskJSON struct {
	Key       domain.TaskKey `json:"key"`
	StartedAt time.Time      `json:"started_at"`
}

type runningJobJSON struct {
	ID        int64          `json:"id"`
	Kind      domain.JobKind `json:"kind"`
	Subject   uuid.UUID      `json:"subject"`
	Attempt   int            `json:"attempt"`
	TitleID   uuid.UUID      `json:"title_id,omitzero"`
	LibraryID uuid.UUID      `json:"library_id,omitzero"`
}

type scanJSON struct {
	LibraryID uuid.UUID        `json:"library_id"`
	Phase     domain.ScanPhase `json:"phase"`
	Done      int              `json:"done"`
	Known     int              `json:"known"`
}

// snapshot is what is going on across the cluster now: tasks and jobs running, libraries being
// scanned, and who is playing what.
func (a *API) snapshot(ctx context.Context) (snapshotJSON, error) {
	out := snapshotJSON{Tasks: []runningTaskJSON{}, Jobs: []runningJobJSON{}, Scans: []scanJSON{}}
	statuses, err := a.svc.Tasks.Statuses(ctx)
	if err != nil {
		return out, err
	}
	for _, s := range statuses {
		if s.Running {
			out.Tasks = append(out.Tasks, runningTaskJSON{Key: s.Key, StartedAt: s.State.Started.UTC()})
		}
	}
	jobs, err := a.svc.Jobs.RunningJobs(ctx)
	if err != nil {
		return out, err
	}
	for _, j := range jobs {
		item, lib := j.About()
		out.Jobs = append(out.Jobs, runningJobJSON{ID: j.ID, Kind: j.Kind, Subject: j.Subject, Attempt: j.Attempts, TitleID: item, LibraryID: lib})
	}
	scans, err := a.svc.Events.Scans(ctx)
	if err != nil {
		return out, err
	}
	for _, s := range scans {
		out.Scans = append(out.Scans, scanJSON{LibraryID: s.Library, Phase: s.Phase, Done: s.Done, Known: s.Known})
	}
	playbacks, err := a.svc.NowPlaying.Playbacks(ctx)
	if err != nil {
		return out, err
	}
	out.Playbacks = playbacksJSON(playbacks)
	return out, nil
}
