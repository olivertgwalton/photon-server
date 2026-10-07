package httpapi

import (
	"context"
	"errors"
	"net/http"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/provider"
	"github.com/olivertgwalton/photon-server/internal/store"
)

type collections interface {
	Collections(ctx context.Context, lib, profile uuid.UUID, offset, limit int) ([]store.Card, int64, error)
	Members(ctx context.Context, profile, collection uuid.UUID) ([]store.Card, error)
	AddCollection(ctx context.Context, lib uuid.UUID, title string) (uuid.UUID, error)
	AddSmartCollection(ctx context.Context, lib uuid.UUID, title string, rule store.SmartRule) (uuid.UUID, error)
	SetRule(ctx context.Context, collection uuid.UUID, rule store.SmartRule) error
	AddListCollection(ctx context.Context, lib uuid.UUID, title string, list store.ListRef, listed []domain.Listed) (uuid.UUID, error)
	SetListMembers(ctx context.Context, collection uuid.UUID, listed []domain.Listed) error
	CollectionList(ctx context.Context, collection uuid.UUID) (store.ListRef, error)
	SetMembers(ctx context.Context, collection uuid.UUID, items []uuid.UUID) error
	SetPlacement(ctx context.Context, collection uuid.UUID, placement domain.CollectionPlacement) error
	RemoveCollection(ctx context.Context, collection uuid.UUID) error
}

// libraryCollections answers a page of a library's collections, by title, as walls page.
func (a *API) libraryCollections(w http.ResponseWriter, r *http.Request) {
	lib, ok := a.pathID(w, r, "id")
	if !ok {
		return
	}
	offset, limit, ok := a.paging(w, r, defaultWallLimit)
	if !ok {
		return
	}
	cards, total, err := a.svc.Collections.Collections(r.Context(), lib, sessionOf(r).Profile.ID, offset, limit)
	if a.answered(w, r, err) {
		return
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, pageJSON[cardJSON]{cardsJSON(cards), offset, total})
}

// members answers a collection's titles.
func (a *API) members(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r, "id")
	if !ok {
		return
	}
	cards, err := a.svc.Collections.Members(r.Context(), sessionOf(r).Profile.ID, id)
	if a.answered(w, r, err) {
		return
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, listJSON[cardJSON]{Items: cardsJSON(cards)})
}

// addCollectionJSON is an admin's collection: titles put in it by hand; given a rule, a smart
// collection of the titles the rule finds, as Plex's; or given a list kept on a provider, the
// titles of it the library has, as Kometa's list builders.
type addCollectionJSON struct {
	LibraryID uuid.UUID        `json:"library_id"`
	Title     string           `json:"title"`
	Rule      *store.SmartRule `json:"rule,omitzero"`
	List      *store.ListRef   `json:"list,omitzero"`
}

// addCollection makes an admin's collection in a library.
func (a *API) addCollection(w http.ResponseWriter, r *http.Request) {
	var req addCollectionJSON
	if !a.decode(w, r, &req) {
		return
	}
	if req.Title == "" {
		writeProblem(w, a.logger, codeInvalidBody, "title is set")
		return
	}
	if req.Rule != nil && req.List != nil {
		writeProblem(w, a.logger, codeInvalidBody, "a collection is made by a rule or a list, not both")
		return
	}
	if req.Rule != nil && !a.checkedRule(w, *req.Rule) {
		return
	}
	var id uuid.UUID
	var err error
	switch {
	case req.Rule != nil:
		id, err = a.svc.Collections.AddSmartCollection(r.Context(), req.LibraryID, req.Title, *req.Rule)
	case req.List != nil:
		listed, ok := a.readList(w, r, *req.List)
		if !ok {
			return
		}
		id, err = a.svc.Collections.AddListCollection(r.Context(), req.LibraryID, req.Title, *req.List, listed)
	default:
		id, err = a.svc.Collections.AddCollection(r.Context(), req.LibraryID, req.Title)
	}
	if errors.Is(err, store.ErrNotFound) {
		writeProblem(w, a.logger, codeInvalidBody, "library_id is not a library")
		return
	}
	if err != nil {
		a.internal(w, r, err)
		return
	}
	writeJSON(w, a.logger, "application/json", http.StatusCreated, createdJSON{ID: id})
}

// setMembers replaces an admin's collection's titles, in the order given.
func (a *API) setMembers(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r, "id")
	if !ok {
		return
	}
	var req itemIDsJSON
	if !a.decode(w, r, &req) {
		return
	}
	err := a.svc.Collections.SetMembers(r.Context(), id, req.ItemIDs)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeProblem(w, a.logger, codeNotFound, "the collection, or one of its titles, is not in its library")
	case a.answered(w, r, err):
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

type placementJSON struct {
	Placement domain.CollectionPlacement `json:"placement"`
}

// setPlacement puts a collection, an admin's or a provider's, on the home page or back in its
// library only.
func (a *API) setPlacement(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r, "id")
	if !ok {
		return
	}
	var req placementJSON
	if !a.decode(w, r, &req) {
		return
	}
	if req.Placement == "" {
		writeProblem(w, a.logger, codeInvalidBody, "placement is set")
		return
	}
	if !a.answered(w, r, a.svc.Collections.SetPlacement(r.Context(), id, req.Placement)) {
		w.WriteHeader(http.StatusNoContent)
	}
}

// removeCollection removes an admin's collection, leaving its titles.
func (a *API) removeCollection(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r, "id")
	if !ok {
		return
	}
	if !a.answered(w, r, a.svc.Collections.RemoveCollection(r.Context(), id)) {
		w.WriteHeader(http.StatusNoContent)
	}
}

// checkedRule refuses a rule a smart collection cannot keep.
func (a *API) checkedRule(w http.ResponseWriter, rule store.SmartRule) bool {
	if err := rule.Check(); err != nil {
		writeProblem(w, a.logger, codeInvalidBody, err.Error())
		return false
	}
	return true
}

// setRule replaces a smart collection's rule, and its titles with what it finds.
func (a *API) setRule(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r, "id")
	if !ok {
		return
	}
	var req store.SmartRule
	if !a.decode(w, r, &req) || !a.checkedRule(w, req) {
		return
	}
	if !a.answered(w, r, a.svc.Collections.SetRule(r.Context(), id, req)) {
		w.WriteHeader(http.StatusNoContent)
	}
}

// readList reads a list kept on a provider, saying why it cannot be: a list named as none is, a
// provider that keeps none or is not set up, or one that did not answer.
func (a *API) readList(w http.ResponseWriter, r *http.Request, list store.ListRef) ([]domain.Listed, bool) {
	if err := list.Check(); err != nil {
		writeProblem(w, a.logger, codeInvalidBody, err.Error())
		return nil, false
	}
	listed, err := a.svc.Providers.List(r.Context(), list.Source, list.ID)
	switch {
	case errors.Is(err, provider.ErrNoLister), errors.Is(err, provider.ErrNotConfigured), errors.Is(err, provider.ErrNotFound):
		writeProblem(w, a.logger, codeInvalidBody, "the list cannot be read: "+err.Error())
		return nil, false
	case err != nil:
		writeProblem(w, a.logger, codeProviderUnavailable, err.Error())
		return nil, false
	}
	return listed, true
}

// syncList reads a list collection's list again now, and keeps what it holds.
func (a *API) syncList(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r, "id")
	if !ok {
		return
	}
	list, err := a.svc.Collections.CollectionList(r.Context(), id)
	if a.answered(w, r, err) {
		return
	}
	listed, ok := a.readList(w, r, list)
	if !ok {
		return
	}
	if !a.answered(w, r, a.svc.Collections.SetListMembers(r.Context(), id, listed)) {
		w.WriteHeader(http.StatusNoContent)
	}
}
