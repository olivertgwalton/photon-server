package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/playback"
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
	RunningJobs(ctx context.Context) ([]store.Job, error)
}

type nowPlaying interface {
	Playbacks(ctx context.Context) ([]domain.Playback, error)
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

// adminTasks lists the scheduled tasks as Jellyfin's dashboard does: how each last ran, whether it
// is running, and when it next will.
func (a *API) adminTasks(w http.ResponseWriter, r *http.Request) {
	statuses, err := a.svc.Tasks.Statuses(r.Context())
	if err != nil {
		a.internal(w, r, err)
		return
	}
	out := make([]taskJSON, len(statuses))
	for i, s := range statuses {
		out[i] = taskJSON{
			Key: s.Key, Running: s.Running, StartedAt: s.State.Started, FinishedAt: s.State.Finished,
			Result: s.State.Result, Error: s.State.Error, NextAt: s.Next,
		}
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, listJSON[taskJSON]{Items: out})
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

type jobCountJSON struct {
	Kind  domain.JobKind  `json:"kind"`
	State domain.JobState `json:"state"`
	Count int             `json:"count"`
}

type deadJobJSON struct {
	ID       int64          `json:"id"`
	Kind     domain.JobKind `json:"kind"`
	Subject  uuid.UUID      `json:"subject"`
	Attempts int            `json:"attempts"`
	Error    string         `json:"error,omitzero"`
}

type jobQueueJSON struct {
	Counts []jobCountJSON `json:"counts"`
	Dead   []deadJobJSON  `json:"dead"`
}

// adminJobs answers the job queue: how many of each kind are in each state, and those that died.
func (a *API) adminJobs(w http.ResponseWriter, r *http.Request) {
	counts, dead, err := a.svc.Jobs.JobQueue(r.Context())
	if err != nil {
		a.internal(w, r, err)
		return
	}
	out := jobQueueJSON{Counts: []jobCountJSON{}, Dead: []deadJobJSON{}}
	for _, c := range counts {
		out.Counts = append(out.Counts, jobCountJSON(c))
	}
	for _, d := range dead {
		out.Dead = append(out.Dead, deadJobJSON{ID: d.ID, Kind: d.Kind, Subject: d.Subject, Attempts: d.Attempts, Error: d.Error})
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

// transcodesJSON is how many videos a node is transcoding, against its limit if it has one, and
// how many of those are download conversions.
type transcodesJSON struct {
	Active      int `json:"active"`
	Conversions int `json:"conversions"`
	Limit       int `json:"limit,omitzero"`
}

type nowPlayingListJSON struct {
	Items      []playback.NowPlaying `json:"items"`
	Transcodes transcodesJSON        `json:"transcodes"`
}

// adminPlaybacks answers who is playing what, how, on which node, and where they have got to, and
// how many videos the node answering is transcoding against its limit, absent where it has none.
func (a *API) adminPlaybacks(w http.ResponseWriter, r *http.Request) {
	all, err := a.svc.NowPlaying.Playbacks(r.Context())
	if err != nil {
		a.internal(w, r, err)
		return
	}
	active, conversions, limit := a.svc.HLS.Transcodes()
	writeJSON(w, a.logger, "application/json", http.StatusOK, nowPlayingListJSON{playbacksJSON(all), transcodesJSON{active, conversions, limit}})
}

func playbacksJSON(all []domain.Playback) []playback.NowPlaying {
	out := make([]playback.NowPlaying, len(all))
	for i, p := range all {
		out[i] = playback.Showing(p)
	}
	return out
}

// stopPlayback ends anyone's playback, as Jellyfin's dashboard stops a session: its remux on the
// node running it, the place and the play kept where its player last said it was.
func (a *API) stopPlayback(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeProblem(w, a.logger, codeNotFound, "")
		return
	}
	err = a.svc.Playbacks.End(r.Context(), id)
	switch {
	case errors.Is(err, playback.ErrNoPlayback):
		writeProblem(w, a.logger, codeNotFound, "the playback has stopped, or lapsed")
	case err != nil:
		a.internal(w, r, err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}
