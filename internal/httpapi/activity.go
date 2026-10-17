package httpapi

import (
	"context"
	"net/http"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
	"github.com/olivertgwalton/photon-server/internal/words"
)

type activityLog interface {
	Activity(ctx context.Context, kind domain.EventKind, offset, limit int) ([]domain.Event, int64, error)
}

type eventHub interface {
	Raise(ctx context.Context, e domain.Event)
	// Changed tells a library's titles that were added, changed or removed outside a scan.
	Changed(ctx context.Context, lib uuid.UUID, titles store.Changed)
	Subscribe() (<-chan domain.Event, func())
	Scans(ctx context.Context) ([]domain.ScanProgress, error)
	Backlogs(ctx context.Context) ([]domain.Backlog, error)
	BacklogStopped(ctx context.Context, kind domain.JobKind) error
	TestWebhook(ctx context.Context, id uuid.UUID) error
}

// eventJSON is an event as the log lists it and the stream tells it; id is its activity entry's.
type eventJSON struct {
	ID        uuid.UUID           `json:"id,omitzero"`
	Kind      domain.EventKind    `json:"kind"`
	At        time.Time           `json:"at"`
	ProfileID uuid.UUID           `json:"profile_id,omitzero"`
	TitleID   uuid.UUID           `json:"title_id,omitzero"`
	LibraryID uuid.UUID           `json:"library_id,omitzero"`
	Details   domain.EventDetails `json:"details"`
	// Text is what happened as a sentence, in the reader's language, on the admin's log and stream.
	Text string `json:"text,omitzero"`
}

func eventOf(e domain.Event) eventJSON {
	out := eventJSON{
		ID: e.ID, Kind: e.Kind, At: e.At.UTC(), ProfileID: e.Profile, TitleID: e.Item, LibraryID: e.Library,
		Details: e.Details,
	}
	if out.Details == nil {
		out.Details = domain.NoDetails{}
	}
	return out
}

// adminActivity answers a page of the activity log, the newest first, as Jellyfin's dashboard
// lists it: every kind, or one.
func (a *API) adminActivity(w http.ResponseWriter, r *http.Request) {
	kind, ok := queryEnum(a, w, r, "kind", "", domain.LoggedEventKinds())
	if !ok {
		return
	}
	offset, limit, ok := a.paging(w, r, defaultWallLimit)
	if !ok {
		return
	}
	entries, total, err := a.svc.Activity.Activity(r.Context(), kind, offset, limit)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	names, err := a.eventNames(r.Context())
	if err != nil {
		a.internal(w, r, err)
		return
	}
	said := words.Negotiate(w, r)
	out := make([]eventJSON, len(entries))
	for i, e := range entries {
		out[i] = eventOf(e)
		out[i].Text = said.Event(e, names)
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, pageJSON[eventJSON]{out, offset, total})
}

type snapshotJSON struct {
	Tasks     []runningTaskJSON   `json:"tasks"`
	Jobs      []runningJobJSON    `json:"jobs"`
	Backlogs  []backlogJSON       `json:"backlogs"`
	Scans     []scanJSON          `json:"scans"`
	Playbacks []domain.NowPlaying `json:"playbacks"`
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

// backlogJSON is how far a kind of job has got: left to run, and done since none was left, so
// done / (done + left) of it is through.
type backlogJSON struct {
	Kind domain.JobKind `json:"kind"`
	Left int            `json:"left"`
	Done int            `json:"done"`
}

type scanJSON struct {
	LibraryID uuid.UUID        `json:"library_id"`
	Phase     domain.ScanPhase `json:"phase"`
	Done      int              `json:"done"`
	Known     int              `json:"known"`
	// Folder is the folder read last, under the library's root; absent for the root itself.
	Folder string `json:"folder,omitempty"`
}

// snapshot is what is going on across the cluster now: tasks and jobs running, how far each kind of
// job's backlog has got, libraries being scanned, and who is playing what.
func (a *API) snapshot(ctx context.Context) (snapshotJSON, error) {
	out := snapshotJSON{Tasks: []runningTaskJSON{}, Jobs: []runningJobJSON{}, Backlogs: []backlogJSON{}, Scans: []scanJSON{}}
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
	backlogs, err := a.svc.Events.Backlogs(ctx)
	if err != nil {
		return out, err
	}
	for _, b := range backlogs {
		out.Backlogs = append(out.Backlogs, backlogJSON{Kind: b.Kind, Left: b.Left, Done: b.Done})
	}
	scans, err := a.svc.Events.Scans(ctx)
	if err != nil {
		return out, err
	}
	for _, s := range scans {
		out.Scans = append(out.Scans, scanJSON{LibraryID: s.Library, Phase: s.Phase, Done: s.Done, Known: s.Known, Folder: s.Folder})
	}
	playbacks, err := a.svc.NowPlaying.Playbacks(ctx)
	if err != nil {
		return out, err
	}
	out.Playbacks = playbacksJSON(playbacks)
	return out, nil
}

func (a *API) activityRoutes() []route {
	return []route{
		{
			pattern: "GET /api/v1/admin/activity", access: admin, summary: "Page the activity log, the newest first",
			query:  append([]param{{"kind", domain.EventKind(""), "Only entries of this kind, one the log keeps."}}, pageParams...),
			status: http.StatusOK, reply: pageJSON[eventJSON]{}, handle: a.adminActivity,
		},
	}
}
