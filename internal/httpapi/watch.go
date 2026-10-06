package httpapi

import (
	"context"
	"net/http"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

type watching interface {
	SaveProgress(ctx context.Context, profile, item uuid.UUID, position time.Duration, before domain.Reach) (domain.Reach, error)
	MarkWatched(ctx context.Context, profile, item uuid.UUID) error
	MarkUnwatched(ctx context.Context, profile, item uuid.UUID) error
	ClearProgress(ctx context.Context, profile, item uuid.UUID) error
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
	id, ok := a.pathID(w, r, "id")
	if !ok {
		return
	}
	reach, err := a.svc.Watching.SaveProgress(r.Context(), sessionOf(r).Profile.ID, id, time.Duration(req.PositionMS)*time.Millisecond, domain.ReachStart)
	if !a.answered(w, r, err) {
		a.titleStateChanged(r, id)
		writeJSON(w, a.logger, "application/json", http.StatusOK, reachedJSON{Reach: reach})
	}
}

// titleStateChanged tells the profile's other devices its own state of a title changed.
func (a *API) titleStateChanged(r *http.Request, id uuid.UUID) {
	profile := sessionOf(r).Profile.ID
	a.svc.Events.Raise(r.Context(), domain.Event{Kind: domain.EventUserDataChanged, Profile: profile, Item: id})
}

// playlistChanged tells the profile's other devices one of its playlists changed.
func (a *API) playlistChanged(r *http.Request, id uuid.UUID) {
	profile := sessionOf(r).Profile.ID
	a.svc.Events.Raise(r.Context(), domain.Event{
		Kind: domain.EventUserDataChanged, Profile: profile, Details: map[string]any{"playlist_id": id},
	})
}

// titleUpdated tells the profiles that see a title it was described again.
func (a *API) titleUpdated(r *http.Request, id uuid.UUID) {
	a.svc.Events.Raise(r.Context(), domain.Event{Kind: domain.EventTitleUpdated, Item: id})
}

// mark answers a route that sets or clears a profile's mark on a title, by one of watching's
// methods.
func (a *API) mark(set func(w watching, ctx context.Context, profile, item uuid.UUID) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := a.pathID(w, r, "id")
		if !ok {
			return
		}
		if !a.answered(w, r, set(a.svc.Watching, r.Context(), sessionOf(r).Profile.ID, id)) {
			a.titleStateChanged(r, id)
			w.WriteHeader(http.StatusNoContent)
		}
	}
}
