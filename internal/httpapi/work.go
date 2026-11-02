package httpapi

import (
	"context"
	"net/http"
	"slices"
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
	StopJobs(ctx context.Context, kinds []domain.JobKind) error
	RunningJobs(ctx context.Context) ([]domain.Job, error)
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
	// Jobs are the kinds of job it queues, whose backlog is how far its work has got.
	Jobs []domain.JobKind `json:"jobs,omitzero"`
}

// adminTasks lists the scheduled tasks as Jellyfin's dashboard does: how each last ran, whether it
// is running, and when it next will.
func (a *API) adminTasks(r *http.Request, _ none) (listJSON[taskJSON], error) {
	statuses, err := a.svc.Tasks.Statuses(r.Context())
	if err != nil {
		return listJSON[taskJSON]{}, err
	}
	out := make([]taskJSON, len(statuses))
	for i, s := range statuses {
		out[i] = taskJSON{
			Key: s.Key, Running: s.Running, StartedAt: s.State.Started, FinishedAt: s.State.Finished,
			Result: s.State.Result, Error: s.State.Error, NextAt: s.Next, Jobs: s.Key.Jobs(),
		}
	}
	return listJSON[taskJSON]{Items: out}, nil
}

// runTask asks for a task to run now, on whichever node schedules tasks.
func (a *API) runTask(r *http.Request, _ none) (none, error) {
	return none{}, a.svc.Tasks.Request(r.Context(), domain.TaskKey(r.PathValue("key")))
}

// stopTask takes the jobs a task queued off the queue, as Jellyfin's dashboard stops a task: what
// it had done stands, and its next run queues what is left.
func (a *API) stopTask(r *http.Request, _ none) (none, error) {
	key := domain.TaskKey(r.PathValue("key"))
	if !slices.Contains(domain.TaskKeys(), key) {
		return none{}, task.ErrNoTask
	}
	kinds := key.Jobs()
	if len(kinds) == 0 {
		return none{}, refuse(codeConflict, "the task does all it does in moments, and queues no work to stop")
	}
	if err := a.svc.Jobs.StopJobs(r.Context(), kinds); err != nil {
		return none{}, err
	}
	for _, kind := range kinds {
		if err := a.svc.Events.BacklogStopped(r.Context(), kind); err != nil {
			return none{}, err
		}
	}
	return none{}, nil
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
func (a *API) adminJobs(r *http.Request, _ none) (jobQueueJSON, error) {
	counts, dead, err := a.svc.Jobs.JobQueue(r.Context())
	if err != nil {
		return jobQueueJSON{}, err
	}
	var out jobQueueJSON
	for _, c := range counts {
		out.Counts = append(out.Counts, jobCountJSON(c))
	}
	for _, d := range dead {
		out.Dead = append(out.Dead, deadJobJSON{ID: d.ID, Kind: d.Kind, Subject: d.Subject, Attempts: d.Attempts, Error: d.Error})
	}
	return out, nil
}

// retryJob gives a dead job a fresh set of attempts.
func (a *API) retryJob(r *http.Request, _ none) (none, error) {
	id, err := pathNumber(r, "id")
	if err != nil {
		return none{}, err
	}
	return none{}, a.svc.Jobs.RetryJob(r.Context(), int64(id))
}

// transcodesJSON is how many videos a node is transcoding, against its limit if it has one, and
// how many of those are download conversions.
type transcodesJSON struct {
	Active      int `json:"active"`
	Conversions int `json:"conversions"`
	Limit       int `json:"limit,omitzero"`
}

type nowPlayingListJSON struct {
	Items      []domain.NowPlaying `json:"items"`
	Transcodes transcodesJSON      `json:"transcodes"`
}

// adminPlaybacks answers who is playing what, how, on which node, and where they have got to, and
// how many videos the node answering is transcoding against its limit, absent where it has none.
func (a *API) adminPlaybacks(r *http.Request, _ none) (nowPlayingListJSON, error) {
	all, err := a.svc.NowPlaying.Playbacks(r.Context())
	if err != nil {
		return nowPlayingListJSON{}, err
	}
	active, conversions, limit := a.svc.HLS.Transcodes()
	return nowPlayingListJSON{playbacksJSON(all), transcodesJSON{active, conversions, limit}}, nil
}

func playbacksJSON(all []domain.Playback) []domain.NowPlaying {
	out := make([]domain.NowPlaying, len(all))
	for i, p := range all {
		out[i] = p.Showing()
	}
	return out
}

// stopPlayback ends anyone's playback, as Jellyfin's dashboard stops a session: its remux on the
// node running it, the place and the play kept where its player last said it was.
func (a *API) stopPlayback(w http.ResponseWriter, r *http.Request) {
	id := pathUUID(r, "id")
	if !a.answered(w, r, a.svc.Playbacks.End(r.Context(), id)) {
		w.WriteHeader(http.StatusNoContent)
	}
}

func (a *API) workRoutes() []route {
	return []route{
		a.handles(route{
			pattern: "GET /api/v1/admin/tasks", access: admin, summary: "List the scheduled tasks and how each last ran",
			status: http.StatusOK,
		}, a.adminTasks),
		a.handles(route{
			pattern: "POST /api/v1/admin/tasks/{key}/run", access: admin, summary: "Run a task now",
			path:   []param{{"key", domain.TaskKey(""), "The task."}},
			status: http.StatusAccepted,
		}, a.runTask),
		a.handles(route{
			pattern: "DELETE /api/v1/admin/tasks/{key}/run", access: admin,
			summary: "Stop a task's run: take the jobs it queued off the queue, running or not",
			path:    []param{{"key", domain.TaskKey(""), "The task."}},
			status:  http.StatusNoContent,
		}, a.stopTask),
		a.handles(route{
			pattern: "GET /api/v1/admin/jobs", access: admin, summary: "Count the job queue and list the dead jobs",
			status: http.StatusOK,
		}, a.adminJobs),
		a.handles(route{
			pattern: "POST /api/v1/admin/jobs/{id}/retry", access: admin, summary: "Retry a dead job",
			path:   []param{{"id", int64(0), "The job."}},
			status: http.StatusAccepted,
		}, a.retryJob),
		a.handles(route{
			pattern: "GET /api/v1/admin/playbacks", access: admin,
			summary: "List who is playing what, and how many videos this node is transcoding",
			status:  http.StatusOK,
		}, a.adminPlaybacks),
		{
			pattern: "DELETE /api/v1/admin/playbacks/{id}", access: admin,
			summary: "Stop anyone's playback, keeping where it had got to", status: http.StatusNoContent,
			handle: a.routeToOwner("id", http.HandlerFunc(a.stopPlayback)).ServeHTTP,
		},
	}
}
