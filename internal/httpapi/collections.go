package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/store"
)

type collections interface {
	Collections(ctx context.Context, lib, profile uuid.UUID, offset, limit int) ([]store.Card, int64, error)
	Members(ctx context.Context, profile, collection uuid.UUID) ([]store.Card, error)
	AddCollection(ctx context.Context, lib uuid.UUID, title string) (uuid.UUID, error)
	SetMembers(ctx context.Context, collection uuid.UUID, items []uuid.UUID) error
	RemoveCollection(ctx context.Context, collection uuid.UUID) error
}

// libraryCollections answers a page of a library's collections, by title, as walls page.
func (a *API) libraryCollections(w http.ResponseWriter, r *http.Request) {
	lib, ok := a.pathID(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	offset, limit := 0, defaultWallLimit
	var err error
	if s := q.Get("offset"); s != "" {
		if offset, err = strconv.Atoi(s); err != nil || offset < 0 {
			writeProblem(w, a.logger, codeInvalidParameter, "offset is a number from 0")
			return
		}
	}
	if s := q.Get("limit"); s != "" {
		if limit, err = strconv.Atoi(s); err != nil || limit < 1 || limit > maxWallLimit {
			writeProblem(w, a.logger, codeInvalidParameter, "limit is a number from 1 to "+strconv.Itoa(maxWallLimit))
			return
		}
	}
	cards, total, err := a.svc.Collections.Collections(r.Context(), lib, sessionOf(r).Profile.ID, offset, limit)
	if a.answered(w, r, err) {
		return
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, pageJSON[cardJSON]{cardsJSON(cards), offset, total})
}

// members answers a collection's titles.
func (a *API) members(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r)
	if !ok {
		return
	}
	cards, err := a.svc.Collections.Members(r.Context(), sessionOf(r).Profile.ID, id)
	if a.answered(w, r, err) {
		return
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, listJSON[cardJSON]{Items: cardsJSON(cards)})
}

type addCollectionJSON struct {
	LibraryID uuid.UUID `json:"library_id"`
	Title     string    `json:"title"`
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
	id, err := a.svc.Collections.AddCollection(r.Context(), req.LibraryID, req.Title)
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
	id, ok := a.pathID(w, r)
	if !ok {
		return
	}
	var req itemIDsJSON
	if !a.decode(w, r, &req) {
		return
	}
	err := a.svc.Collections.SetMembers(r.Context(), id, req.ItemIDs)
	switch {
	case errors.Is(err, store.ErrNotUserCollection):
		writeProblem(w, a.logger, codeConflict, err.Error())
	case errors.Is(err, store.ErrNotFound):
		writeProblem(w, a.logger, codeNotFound, "the collection, or one of its titles, is not in its library")
	case err != nil:
		a.internal(w, r, err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// removeCollection removes an admin's collection, leaving its titles.
func (a *API) removeCollection(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r)
	if !ok {
		return
	}
	err := a.svc.Collections.RemoveCollection(r.Context(), id)
	switch {
	case errors.Is(err, store.ErrNotUserCollection):
		writeProblem(w, a.logger, codeConflict, err.Error())
	case a.answered(w, r, err):
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}
