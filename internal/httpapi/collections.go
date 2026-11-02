package httpapi

import (
	"context"
	"errors"
	"net/http"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/auth"
	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/provider"
	"github.com/olivertgwalton/photon-server/internal/store"
)

type collections interface {
	Collections(ctx context.Context, lib, profile uuid.UUID, page store.Page) ([]store.Card, store.Extent, error)
	AllCollections(ctx context.Context, profile uuid.UUID, page store.Page) ([]store.Card, store.Extent, error)
	Members(ctx context.Context, profile, collection uuid.UUID) ([]store.Card, error)
	AddCollection(ctx context.Context, title string) (uuid.UUID, error)
	AddSmartCollection(ctx context.Context, libs []uuid.UUID, title string, rule store.SmartRule) (uuid.UUID, error)
	SetRule(ctx context.Context, collection uuid.UUID, rule store.SmartRule) error
	AddListCollection(ctx context.Context, libs []uuid.UUID, title string, list store.ListRef, listed []domain.Listed) (uuid.UUID, error)
	SetListMembers(ctx context.Context, collection uuid.UUID, listed []domain.Listed) error
	CollectionList(ctx context.Context, collection uuid.UUID) (store.ListRef, error)
	SetMembers(ctx context.Context, collection uuid.UUID, items []uuid.UUID) error
	AddMembers(ctx context.Context, collection uuid.UUID, items []uuid.UUID) error
	RemoveCollection(ctx context.Context, collection uuid.UUID) error
	SetBoxSet(ctx context.Context, collection uuid.UUID, mode domain.CollectionBoxSet) error
}

// allCollections answers a page of every collection the profile is shown, by title: what the
// Collections view lists.
func (a *API) allCollections(r *http.Request, _ none) (pageJSON[cardJSON], error) {
	page, err := paging(r, defaultWallLimit)
	if err != nil {
		return pageJSON[cardJSON]{}, err
	}
	cards, extent, err := a.svc.Collections.AllCollections(r.Context(), auth.SessionOf(r.Context()).Profile.ID, page)
	if err != nil {
		return pageJSON[cardJSON]{}, err
	}
	return pageAt(cardsJSON(cards), page.Offset, extent), nil
}

// libraryCollections answers a page of the collections holding a library's titles, by title, as
// walls page.
func (a *API) libraryCollections(r *http.Request, _ none) (pageJSON[cardJSON], error) {
	lib := pathUUID(r, "id")
	page, err := paging(r, defaultWallLimit)
	if err != nil {
		return pageJSON[cardJSON]{}, err
	}
	cards, extent, err := a.svc.Collections.Collections(r.Context(), lib, auth.SessionOf(r.Context()).Profile.ID, page)
	if err != nil {
		return pageJSON[cardJSON]{}, err
	}
	return pageAt(cardsJSON(cards), page.Offset, extent), nil
}

// members answers a collection's titles.
func (a *API) members(r *http.Request, _ none) (listJSON[cardJSON], error) {
	id := pathUUID(r, "id")
	cards, err := a.svc.Collections.Members(r.Context(), auth.SessionOf(r.Context()).Profile.ID, id)
	if err != nil {
		return listJSON[cardJSON]{}, err
	}
	return listJSON[cardJSON]{Items: cardsJSON(cards)}, nil
}

// addCollectionJSON is an admin's collection, in no library: titles of any put in it by hand;
// given a rule, a smart collection of the titles the rule finds in LibraryIDs, as Plex's; or given
// a list kept on a provider, the titles of it LibraryIDs have, as Kometa's list builders.
type addCollectionJSON struct {
	Title      string         `json:"title"`
	Rule       *smartRuleJSON `json:"rule,omitzero"`
	List       *listRefJSON   `json:"list,omitzero"`
	LibraryIDs []uuid.UUID    `json:"library_ids,omitzero"`
}

// addCollection makes an admin's collection.
func (a *API) addCollection(r *http.Request, req addCollectionJSON) (createdJSON, error) {
	if req.Title == "" {
		return createdJSON{}, refuse(codeInvalidBody, "title is set")
	}
	if req.Rule != nil && req.List != nil {
		return createdJSON{}, refuse(codeInvalidBody, "a collection is made by a rule or a list, not both")
	}
	if (req.Rule != nil || req.List != nil) != (len(req.LibraryIDs) > 0) {
		return createdJSON{}, refuse(codeInvalidBody, "library_ids names the libraries a rule or a list reads, and a collection made by hand none")
	}
	if req.Rule != nil {
		if err := req.Rule.rule().CheckShared(); err != nil {
			return createdJSON{}, refuse(codeInvalidBody, err.Error())
		}
	}
	var id uuid.UUID
	var err error
	switch {
	case req.Rule != nil:
		id, err = a.svc.Collections.AddSmartCollection(r.Context(), req.LibraryIDs, req.Title, req.Rule.rule())
	case req.List != nil:
		var listed []domain.Listed
		if listed, err = a.readList(r, req.List.ref()); err != nil {
			return createdJSON{}, err
		}
		id, err = a.svc.Collections.AddListCollection(r.Context(), req.LibraryIDs, req.Title, req.List.ref(), listed)
	default:
		id, err = a.svc.Collections.AddCollection(r.Context(), req.Title)
	}
	if errors.Is(err, store.ErrNotFound) {
		return createdJSON{}, refuse(codeInvalidBody, "library_ids names one library or more, each once")
	}
	if err != nil {
		return createdJSON{}, err
	}
	return createdJSON{ID: id}, nil
}

// setMembers replaces an admin's collection's titles, in the order given.
func (a *API) addMembers(r *http.Request, req itemIDsJSON) (none, error) {
	id := pathUUID(r, "id")
	err := a.svc.Collections.AddMembers(r.Context(), id, req.ItemIDs)
	return none{}, notFoundAs(err, "the collection, or one of its titles, is not in its library")
}

func (a *API) setMembers(r *http.Request, req itemIDsJSON) (none, error) {
	id := pathUUID(r, "id")
	err := a.svc.Collections.SetMembers(r.Context(), id, req.ItemIDs)
	return none{}, notFoundAs(err, "the collection, or one of its titles, is not in its library")
}

// removeCollection removes an admin's collection, leaving its titles.
func (a *API) removeCollection(r *http.Request, _ none) (none, error) {
	id := pathUUID(r, "id")
	return none{}, a.svc.Collections.RemoveCollection(r.Context(), id)
}

type collectionChangeJSON struct {
	// BoxSet is how a library's wall shows it: library, as the library shows its box sets;
	// collapsed, as one tile in place of its titles; expanded, its titles each on their own; or
	// beside, its tile among its titles.
	BoxSet domain.CollectionBoxSet `json:"box_set"`
}

// setCollection changes how a library's wall shows a collection, whoever made it.
func (a *API) setCollection(r *http.Request, req collectionChangeJSON) (none, error) {
	id := pathUUID(r, "id")
	if req.BoxSet == "" {
		return none{}, refuse(codeInvalidBody, "box_set is missing")
	}
	return none{}, a.svc.Collections.SetBoxSet(r.Context(), id, req.BoxSet)
}

// setRule replaces a smart collection's rule, and its titles with what it finds.
func (a *API) setRule(r *http.Request, req smartRuleJSON) (none, error) {
	if err := req.rule().CheckShared(); err != nil {
		return none{}, refuse(codeInvalidBody, err.Error())
	}
	return none{}, a.svc.Collections.SetRule(r.Context(), pathUUID(r, "id"), req.rule())
}

// readList reads a list kept on a provider, saying why it cannot be: a list named as none is, a
// provider that keeps none or is not set up, or one that did not answer.
func (a *API) readList(r *http.Request, list store.ListRef) ([]domain.Listed, error) {
	if err := list.Check(); err != nil {
		return nil, refuse(codeInvalidBody, err.Error())
	}
	listed, err := a.svc.Providers.List(r.Context(), list.Source, list.ID)
	switch {
	case errors.Is(err, provider.ErrNoLister), errors.Is(err, provider.ErrNotConfigured), errors.Is(err, provider.ErrNotFound):
		return nil, refuse(codeInvalidBody, "the list cannot be read: "+err.Error())
	case err != nil:
		return nil, refuse(codeProviderUnavailable, err.Error())
	}
	return listed, nil
}

// keptListJSON is a list kept on a provider, which a list collection could follow by its source
// and id.
type keptListJSON struct {
	Source domain.FieldSource `json:"source"`
	ID     string             `json:"id"`
	Title  string             `json:"title"`
	About  string             `json:"about,omitzero"`
	Holds  domain.ItemKind    `json:"holds,omitzero" doc:"movie or show; absent for both"`
	Size   int                `json:"size,omitzero"`
}

// keptLists answers the lists a provider keeps that a collection could follow, read from it now,
// as Silo's collection templates offer them.
func (a *API) keptLists(r *http.Request, _ none) (listJSON[keptListJSON], error) {
	source := domain.FieldSource(r.URL.Query().Get("source"))
	lists, err := a.svc.Providers.Lists(r.Context(), source, r.URL.Query().Get("query"))
	switch {
	case errors.Is(err, provider.ErrNoLister), errors.Is(err, provider.ErrNotConfigured):
		return listJSON[keptListJSON]{}, refuse(codeInvalidParameter, "source cannot offer its lists: "+err.Error())
	case err != nil:
		return listJSON[keptListJSON]{}, refuse(codeProviderUnavailable, err.Error())
	}
	out := make([]keptListJSON, len(lists))
	for i, l := range lists {
		out[i] = keptListJSON{Source: source, ID: l.ID, Title: l.Title, About: l.About, Holds: l.Holds, Size: l.Size}
	}
	return listJSON[keptListJSON]{Items: out}, nil
}

// syncList reads a list collection's list again now, and keeps what it holds.
func (a *API) syncList(r *http.Request, _ none) (none, error) {
	id := pathUUID(r, "id")
	list, err := a.svc.Collections.CollectionList(r.Context(), id)
	if err != nil {
		return none{}, err
	}
	listed, err := a.readList(r, list)
	if err != nil {
		return none{}, err
	}
	return none{}, a.svc.Collections.SetListMembers(r.Context(), id, listed)
}

func (a *API) collectionsRoutes() []route {
	return []route{
		a.handles(route{
			pattern: "GET /api/v1/collections", access: signedIn, summary: "Page every collection, in the Collections view",
			query: pageParams, status: http.StatusOK,
		}, a.allCollections),
		a.handles(route{
			pattern: "GET /api/v1/libraries/{id}/collections", access: signedIn, summary: "Page the collections holding a library's titles",
			query: pageParams, status: http.StatusOK,
		}, a.libraryCollections),
		a.handles(route{
			pattern: "GET /api/v1/titles/{id}/members", access: signedIn, summary: "A collection's titles",
			status: http.StatusOK,
		}, a.members),
		a.handles(route{
			pattern: "GET /api/v1/admin/lists", access: admin, summary: "The lists a provider keeps that a collection could follow",
			query: []param{
				{"source", domain.FieldSource(""), "The provider: tmdb, mdblist, or a plugin that keeps lists; required."},
				{"query", "", "Lists named like this; by default the provider's own choice, as its best liked."},
			},
			status: http.StatusOK,
		}, a.keptLists),
		a.handles(route{
			pattern: "POST /api/v1/admin/collections", access: admin, summary: "Make a collection, of the titles of any library",
			status: http.StatusCreated,
		}, a.addCollection),
		a.handles(route{
			pattern: "POST /api/v1/admin/collections/{id}/members", access: admin,
			summary: "Put titles at the end of an admin's collection: films, or shows, seasons and episodes",
			status:  http.StatusNoContent,
		}, a.addMembers),
		a.handles(route{
			pattern: "PUT /api/v1/admin/collections/{id}/members", access: admin,
			summary: "Replace an admin's collection's titles, in order",
			status:  http.StatusNoContent,
		}, a.setMembers),
		a.handles(route{
			pattern: "PUT /api/v1/admin/collections/{id}/rule", access: admin,
			summary: "Replace a smart collection's rule, and its titles with what it finds",
			status:  http.StatusNoContent,
		}, a.setRule),
		a.handles(route{
			pattern: "POST /api/v1/admin/collections/{id}/sync", access: admin,
			summary: "Read a list collection's list again now, and keep the titles of it the library has",
			status:  http.StatusNoContent,
		}, a.syncList),
		a.handles(route{
			pattern: "PATCH /api/v1/admin/collections/{id}", access: admin,
			summary: "Change how a library's wall shows a collection, whoever made it",
			status:  http.StatusNoContent,
		}, a.setCollection),
		a.handles(route{
			pattern: "DELETE /api/v1/admin/collections/{id}", access: admin,
			summary: "Remove an admin's collection, leaving its titles", status: http.StatusNoContent,
		}, a.removeCollection),
	}
}
