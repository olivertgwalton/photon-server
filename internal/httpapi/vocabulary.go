package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/words"
)

// vocabularyJSON is what the API's values are called, in the reader's language: one fetch a client
// keeps, rather than a name on every object, for the values it shows and the ones it offers.
type vocabularyJSON struct {
	Tasks everyKey[domain.TaskKey, describedJSON] `json:"tasks"`
	Jobs  everyKey[domain.JobKind, string]        `json:"jobs"`
	// Rows are the kinds of home row, as each one's own page heads it.
	Rows everyKey[domain.HomeRow, string] `json:"rows"`
	// LibraryRows are how a row of one library's titles is headed, {library} its name.
	LibraryRows   map[domain.HomeRow]string                       `json:"library_rows"`
	Roles         everyKey[domain.Role, string]                   `json:"roles"`
	Markers       everyKey[domain.MarkerKind, string]             `json:"markers"`
	Extras        everyKey[domain.ExtraKind, string]              `json:"extras"`
	Reasons       everyKey[domain.TranscodeReason, describedJSON] `json:"reasons"`
	Accelerations everyKey[domain.Acceleration, string]           `json:"accelerations"`
	NodeRoles     everyKey[domain.NodeRole, describedJSON]        `json:"node_roles"`
	ImportSources everyKey[domain.ImportSource, string]           `json:"import_sources"`
	ImportMisses  everyKey[domain.ImportMiss, string]             `json:"import_misses"`
	Trackers      everyKey[domain.Tracker, string]                `json:"trackers"`
	PlayMethods   everyKey[domain.PlayMethod, string]             `json:"play_methods"`
	RatingSites   everyKey[domain.RatingSite, string]             `json:"rating_sites"`
	Ranges        everyKey[domain.Range, string]                  `json:"ranges"`
	Resolutions   everyKey[domain.Resolution, string]             `json:"resolutions"`
	// Kinds are the kinds of item, as a list of mixed titles heads each kind's: "Films".
	Kinds           everyKey[domain.ItemKind, string]       `json:"kinds"`
	LibraryKinds    everyKey[domain.LibraryKind, string]    `json:"library_kinds"`
	StreamKinds     everyKey[domain.StreamKind, string]     `json:"stream_kinds"`
	Marks           everyKey[domain.Mark, string]           `json:"marks"`
	Milestones      everyKey[domain.Milestone, string]      `json:"milestones"`
	CalendarFilters everyKey[domain.CalendarFilter, string] `json:"calendar_filters"`
	Sorts           everyKey[domain.WallSort, sortJSON]     `json:"sorts"`
	JobStates       everyKey[domain.JobState, string]       `json:"job_states"`
	DownloadStates  everyKey[domain.DownloadState, string]  `json:"download_states"`
	// Logged are the kinds of event the activity log keeps, as its filter names them.
	Logged map[domain.EventKind]string `json:"logged"`
	// Hookable are the kinds of event a webhook may ask for, each as what it is told of.
	Hookable map[domain.EventKind]string `json:"hookable"`
}

// everyKey holds a value for every value of its key's enum, so a client may index it by any of them.
type everyKey[K ~string, V any] map[K]V

type describedJSON struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type sortJSON struct {
	Name       string `json:"name"`
	Ascending  string `json:"ascending"`
	Descending string `json:"descending"`
}

func (a *API) vocabulary(w http.ResponseWriter, r *http.Request) {
	said := words.Negotiate(w, r)
	writeJSON(w, a.logger, "application/json", http.StatusOK, vocabularyJSON{
		Tasks:           wordsFor(domain.TaskKeys(), func(k domain.TaskKey) describedJSON { return describedJSON(said.Task(k)) }),
		Jobs:            wordsFor(domain.JobKinds(), said.JobKind),
		Rows:            wordsFor(domain.HomeRows(), said.HomeRow),
		LibraryRows:     libraryRowWords(said),
		Roles:           wordsFor(domain.Roles(), said.Role),
		Markers:         wordsFor(domain.MarkerKinds(), said.MarkerKind),
		Extras:          wordsFor(domain.ExtraKinds(), said.ExtraKind),
		Reasons:         wordsFor(domain.TranscodeReasons(), func(r domain.TranscodeReason) describedJSON { return describedJSON(said.TranscodeReason(r)) }),
		Accelerations:   wordsFor(domain.Accelerations(), said.Acceleration),
		NodeRoles:       wordsFor(domain.NodeRoles(), func(r domain.NodeRole) describedJSON { return describedJSON(said.NodeRole(r)) }),
		ImportSources:   wordsFor(domain.ImportSources(), said.ImportSource),
		ImportMisses:    wordsFor(domain.ImportMisses(), said.ImportMiss),
		Trackers:        wordsFor(domain.Trackers(), said.Tracker),
		PlayMethods:     wordsFor(domain.PlayMethods(), said.PlayMethod),
		RatingSites:     wordsFor(domain.RatingSites(), said.RatingSite),
		Ranges:          wordsFor(domain.Ranges(), said.Range),
		Resolutions:     wordsFor(domain.Resolutions(), said.Resolution),
		Kinds:           wordsFor(domain.ItemKinds(), said.ItemKind),
		LibraryKinds:    wordsFor(domain.LibraryKinds(), said.LibraryKind),
		StreamKinds:     wordsFor(domain.StreamKinds(), said.StreamKind),
		Marks:           wordsFor(domain.Marks(), said.Mark),
		Milestones:      wordsFor(domain.Milestones(), said.Milestone),
		CalendarFilters: wordsFor(domain.CalendarFilters(), said.CalendarFilter),
		Sorts:           wordsFor(domain.WallSorts(), func(s domain.WallSort) sortJSON { return sortJSON(said.Sort(s)) }),
		JobStates:       wordsFor(domain.JobStates(), said.JobState),
		DownloadStates:  wordsFor(domain.DownloadStates(), said.DownloadState),
		Logged:          wordsFor(domain.LoggedEventKinds(), said.Kept),
		Hookable:        wordsFor(domain.HookableEventKinds(), said.Told),
	})
}

func libraryRowWords(said words.Words) map[domain.HomeRow]string {
	out := map[domain.HomeRow]string{}
	for _, k := range domain.HomeRows() {
		if k.LibraryKinds() != nil {
			out[k] = said.LibraryRow(k)
		}
	}
	return out
}

func wordsFor[T comparable, W any](values []T, word func(T) W) map[T]W {
	out := make(map[T]W, len(values))
	for _, v := range values {
		out[v] = word(v)
	}
	return out
}

// eventNames are the profiles' and libraries' names an event is worded with, read once and read
// again for an id they lack, which one added since they were read is.
type eventNames struct {
	ctx                 context.Context
	a                   *API
	profiles, libraries map[uuid.UUID]string
	asked               map[uuid.UUID]bool
}

func (a *API) eventNames(ctx context.Context) (*eventNames, error) {
	n := &eventNames{ctx: ctx, a: a, asked: map[uuid.UUID]bool{}}
	return n, n.read()
}

func (n *eventNames) read() error {
	profiles, err := n.a.svc.Profiles.Profiles(n.ctx)
	if err != nil {
		return err
	}
	libraries, err := n.a.svc.Libraries.Libraries(n.ctx)
	if err != nil {
		return err
	}
	n.profiles, n.libraries = map[uuid.UUID]string{}, map[uuid.UUID]string{}
	for _, p := range profiles {
		n.profiles[p.Profile.ID] = p.Profile.Name
	}
	for _, l := range libraries {
		n.libraries[l.ID] = l.Name
	}
	return nil
}

func (n *eventNames) Profile(id uuid.UUID) string {
	return n.lookUp(func() map[uuid.UUID]string { return n.profiles }, id)
}

func (n *eventNames) Library(id uuid.UUID) string {
	return n.lookUp(func() map[uuid.UUID]string { return n.libraries }, id)
}

// lookUp reads the names again for an id they lack, once for each id, as one gone stays gone.
func (n *eventNames) lookUp(names func() map[uuid.UUID]string, id uuid.UUID) string {
	if name, ok := names()[id]; ok || n.asked[id] {
		return name
	}
	n.asked[id] = true
	if err := n.read(); err != nil {
		n.a.logger.WarnContext(n.ctx, "names not read again", slog.Any("err", err))
	}
	return names()[id]
}

func (a *API) vocabularyRoutes() []route {
	return []route{
		{
			pattern: "GET /api/v1/words", access: signedIn,
			summary: "Say what the API's values are called, in the reader's language",
			status:  http.StatusOK, reply: vocabularyJSON{}, handle: a.vocabulary,
		},
	}
}
