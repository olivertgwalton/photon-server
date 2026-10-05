package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
)

type watching interface {
	SaveProgress(ctx context.Context, profile, item uuid.UUID, position time.Duration) (domain.Reach, error)
	MarkWatched(ctx context.Context, profile, item uuid.UUID) error
	MarkUnwatched(ctx context.Context, profile, item uuid.UUID) error
	Favourite(ctx context.Context, profile, item uuid.UUID) error
	Unfavourite(ctx context.Context, profile, item uuid.UUID) error
}

// progress records where the profile stopped a film or episode, and answers how far that got.
func (a *API) progress(w http.ResponseWriter, r *http.Request) {
	var req positionJSON
	if !a.decode(w, r, &req) {
		return
	}
	if req.PositionMS < 0 {
		writeProblem(w, a.logger, codeInvalidBody, "position_ms is not negative")
		return
	}
	id, ok := a.titleID(w, r)
	if !ok {
		return
	}
	reach, err := a.svc.Watching.SaveProgress(r.Context(), sessionOf(r).Profile.ID, id, time.Duration(req.PositionMS)*time.Millisecond)
	if !a.answered(w, r, err) {
		writeJSON(w, a.logger, "application/json", http.StatusOK, reachJSON{Reach: reach})
	}
}

// mark answers a route that sets or clears a profile's mark on a title, by one of watching's
// methods.
func (a *API) mark(set func(w watching, ctx context.Context, profile, item uuid.UUID) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := a.titleID(w, r)
		if !ok {
			return
		}
		if !a.answered(w, r, set(a.svc.Watching, r.Context(), sessionOf(r).Profile.ID, id)) {
			w.WriteHeader(http.StatusNoContent)
		}
	}
}

func (a *API) titleID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeProblem(w, a.logger, codeNotFound, "")
	}
	return id, err == nil
}

// answered writes the problem err is, if it is one.
func (a *API) answered(w http.ResponseWriter, r *http.Request, err error) bool {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeProblem(w, a.logger, codeNotFound, "")
	case err != nil:
		a.internal(w, r, err)
	default:
		return false
	}
	return true
}
