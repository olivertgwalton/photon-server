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
	Tasks map[domain.TaskKey]describedJSON `json:"tasks"`
	Jobs  map[domain.JobKind]string        `json:"jobs"`
	// Rows are the kinds of home row, as each one's own page heads it.
	Rows          map[domain.HomeRow]string                `json:"rows"`
	Roles         map[domain.Role]string                   `json:"roles"`
	Markers       map[domain.MarkerKind]string             `json:"markers"`
	Extras        map[domain.ExtraKind]string              `json:"extras"`
	Reasons       map[domain.TranscodeReason]describedJSON `json:"reasons"`
	Accelerations map[domain.Acceleration]string           `json:"accelerations"`
	NodeRoles     map[domain.NodeRole]describedJSON        `json:"node_roles"`
	ImportSources map[domain.ImportSource]string           `json:"import_sources"`
	ImportMisses  map[domain.ImportMiss]string             `json:"import_misses"`
	PlayMethods   map[domain.PlayMethod]string             `json:"play_methods"`
	RatingSites   map[domain.RatingSite]string             `json:"rating_sites"`
	Ranges        map[domain.Range]string                  `json:"ranges"`
	Resolutions   map[domain.Resolution]string             `json:"resolutions"`
	// Kinds are the kinds of item, as a list of mixed titles heads each kind's: "Films".
	Kinds           map[domain.ItemKind]string       `json:"kinds"`
	LibraryKinds    map[domain.LibraryKind]string    `json:"library_kinds"`
	StreamKinds     map[domain.StreamKind]string     `json:"stream_kinds"`
	Marks           map[domain.Mark]string           `json:"marks"`
	Milestones      map[domain.Milestone]string      `json:"milestones"`
	CalendarFilters map[domain.CalendarFilter]string `json:"calendar_filters"`
	Sorts           map[domain.WallSort]sortJSON     `json:"sorts"`
	JobStates       map[domain.JobState]string       `json:"job_states"`
	DownloadStates  map[domain.DownloadState]string  `json:"download_states"`
	// Logged are the kinds of event the activity log keeps, as its filter names them.
	Logged map[domain.EventKind]string `json:"logged"`
	// Hookable are the kinds of event a webhook may ask for, each as what it is told of.
	Hookable map[domain.EventKind]string `json:"hookable"`
}

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
		Jobs:            namesOf(said, domain.JobKinds()),
		Rows:            namesOf(said, domain.HomeRows()),
		Roles:           namesOf(said, domain.Roles()),
		Markers:         namesOf(said, domain.MarkerKinds()),
		Extras:          namesOf(said, domain.ExtraKinds()),
		Reasons:         wordsFor(domain.TranscodeReasons(), func(r domain.TranscodeReason) describedJSON { return describedJSON(said.TranscodeReason(r)) }),
		Accelerations:   namesOf(said, domain.Accelerations()),
		NodeRoles:       wordsFor(domain.NodeRoles(), func(r domain.NodeRole) describedJSON { return describedJSON(said.NodeRole(r)) }),
		ImportSources:   namesOf(said, domain.ImportSources()),
		ImportMisses:    namesOf(said, domain.ImportMisses()),
		PlayMethods:     namesOf(said, domain.PlayMethods()),
		RatingSites:     namesOf(said, domain.RatingSites()),
		Ranges:          namesOf(said, domain.Ranges()),
		Resolutions:     namesOf(said, domain.Resolutions()),
		Kinds:           namesOf(said, domain.ItemKinds()),
		LibraryKinds:    namesOf(said, domain.LibraryKinds()),
		StreamKinds:     namesOf(said, domain.StreamKinds()),
		Marks:           namesOf(said, domain.Marks()),
		Milestones:      namesOf(said, domain.Milestones()),
		CalendarFilters: namesOf(said, domain.CalendarFilters()),
		Sorts:           wordsFor(domain.WallSorts(), func(s domain.WallSort) sortJSON { return sortJSON(said.Sort(s)) }),
		JobStates:       namesOf(said, domain.JobStates()),
		DownloadStates:  namesOf(said, domain.DownloadStates()),
		Logged:          wordsFor(domain.LoggedEventKinds(), said.Kept),
		Hookable:        wordsFor(domain.HookableEventKinds(), said.Told),
	})
}

func namesOf[T comparable](said words.Words, values []T) map[T]string {
	return wordsFor(values, func(v T) string { return said.Name(v) })
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
