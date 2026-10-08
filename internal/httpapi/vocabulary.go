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
	Tasks map[domain.TaskKey]taskWordsJSON `json:"tasks"`
	Jobs  map[domain.JobKind]string        `json:"jobs"`
	// Rows are the kinds of home row, as each one's own page heads it.
	Rows map[domain.HomeRow]string `json:"rows"`
}

type taskWordsJSON struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

func (a *API) vocabulary(w http.ResponseWriter, r *http.Request) {
	said := words.Negotiate(w, r)
	out := vocabularyJSON{Tasks: map[domain.TaskKey]taskWordsJSON{}, Jobs: map[domain.JobKind]string{}, Rows: map[domain.HomeRow]string{}}
	for _, k := range domain.TaskKeys() {
		name, does := said.Task(k)
		out.Tasks[k] = taskWordsJSON{Name: name, Description: does}
	}
	for _, k := range domain.JobKinds() {
		out.Jobs[k] = said.Job(k)
	}
	for _, k := range domain.HomeRows() {
		out.Rows[k] = said.Row(k)
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, out)
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
