package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
	"github.com/olivertgwalton/photon-server/internal/task"
)

type tasks interface {
	Statuses(ctx context.Context) ([]task.Status, error)
	Request(ctx context.Context, key domain.TaskKey) error
}

type jobQueue interface {
	JobQueue(ctx context.Context) ([]store.JobCount, []store.DeadJob, error)
	RetryJob(ctx context.Context, id int64) error
}

type nowPlaying interface {
	Playbacks(ctx context.Context) ([]domain.Playback, error)
}

// adminTasks lists the scheduled tasks as Jellyfin's dashboard does: how each last ran, whether it
// is running, and when it next will.
func (a *API) adminTasks(w http.ResponseWriter, r *http.Request) {
	statuses, err := a.svc.Tasks.Statuses(r.Context())
	if err != nil {
		a.internal(w, r, err)
		return
	}
	type taskJSON struct {
		Key        domain.TaskKey    `json:"key"`
		Running    bool              `json:"running"`
		StartedAt  time.Time         `json:"started_at,omitzero"`
		FinishedAt time.Time         `json:"finished_at,omitzero"`
		Result     domain.TaskResult `json:"result,omitzero"`
		Error      string            `json:"error,omitzero"`
		NextAt     time.Time         `json:"next_at"`
	}
	out := make([]taskJSON, len(statuses))
	for i, s := range statuses {
		out[i] = taskJSON{
			Key: s.Key, Running: s.Running, StartedAt: s.State.Started, FinishedAt: s.State.Finished,
			Result: s.State.Result, Error: s.State.Error, NextAt: s.Next,
		}
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, map[string]any{"items": out})
}

// runTask asks for a task to run now, on whichever node schedules tasks.
func (a *API) runTask(w http.ResponseWriter, r *http.Request) {
	err := a.svc.Tasks.Request(r.Context(), domain.TaskKey(r.PathValue("key")))
	switch {
	case errors.Is(err, task.ErrNoTask):
		writeProblem(w, a.logger, codeNotFound, "")
	case err != nil:
		a.internal(w, r, err)
	default:
		w.WriteHeader(http.StatusAccepted)
	}
}

// adminJobs answers the job queue: how many of each kind are in each state, and those that died.
func (a *API) adminJobs(w http.ResponseWriter, r *http.Request) {
	counts, dead, err := a.svc.Jobs.JobQueue(r.Context())
	if err != nil {
		a.internal(w, r, err)
		return
	}
	type countJSON struct {
		Kind  domain.JobKind  `json:"kind"`
		State domain.JobState `json:"state"`
		Count int             `json:"count"`
	}
	type deadJSON struct {
		ID       int64          `json:"id"`
		Kind     domain.JobKind `json:"kind"`
		Subject  uuid.UUID      `json:"subject"`
		Attempts int            `json:"attempts"`
		Error    string         `json:"error,omitzero"`
	}
	out := struct {
		Counts []countJSON `json:"counts"`
		Dead   []deadJSON  `json:"dead"`
	}{Counts: []countJSON{}, Dead: []deadJSON{}}
	for _, c := range counts {
		out.Counts = append(out.Counts, countJSON(c))
	}
	for _, d := range dead {
		out.Dead = append(out.Dead, deadJSON{ID: d.ID, Kind: d.Kind, Subject: d.Subject, Attempts: d.Attempts, Error: d.Error})
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, out)
}

// retryJob gives a dead job a fresh set of attempts.
func (a *API) retryJob(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeProblem(w, a.logger, codeNotFound, "")
		return
	}
	if a.answered(w, r, a.svc.Jobs.RetryJob(r.Context(), id)) {
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// adminPlaybacks answers who is playing what, how, and where they have got to.
func (a *API) adminPlaybacks(w http.ResponseWriter, r *http.Request) {
	all, err := a.svc.NowPlaying.Playbacks(r.Context())
	if err != nil {
		a.internal(w, r, err)
		return
	}
	type playbackJSON struct {
		ID         uuid.UUID         `json:"id"`
		ProfileID  uuid.UUID         `json:"profile_id"`
		TitleID    uuid.UUID         `json:"title_id"`
		VersionID  uuid.UUID         `json:"version_id"`
		Method     domain.PlayMethod `json:"method"`
		State      domain.PlayState  `json:"state"`
		PositionMS int64             `json:"position_ms"`
		StartedAt  time.Time         `json:"started_at"`
		UpdatedAt  time.Time         `json:"updated_at"`
	}
	out := make([]playbackJSON, len(all))
	for i, p := range all {
		out[i] = playbackJSON{
			ID: p.ID, ProfileID: p.Profile, TitleID: p.Item, VersionID: p.Version, Method: p.Method, State: p.State,
			PositionMS: p.Position.Milliseconds(), StartedAt: p.Started.UTC(), UpdatedAt: p.Updated.UTC(),
		}
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, map[string]any{"items": out})
}
