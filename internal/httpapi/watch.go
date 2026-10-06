package httpapi

import (
	"context"
	"net/http"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

type watching interface {
	SaveProgress(ctx context.Context, profile, item uuid.UUID, position time.Duration, before domain.Reach, at *time.Time) (domain.Reach, error)
	MarkWatched(ctx context.Context, profile, item uuid.UUID, at *time.Time) error
	MarkUnwatched(ctx context.Context, profile, item uuid.UUID) error
	ClearProgress(ctx context.Context, profile, item uuid.UUID) error
	Favourite(ctx context.Context, profile, item uuid.UUID) error
	Unfavourite(ctx context.Context, profile, item uuid.UUID) error
}

// clockSkew is how far ahead of the server's a client's clock may run; atRule says it.
const clockSkew = 5 * time.Minute

const atRule = "at is after 1970 and no more than 5 minutes ahead of the server's clock"

// atValid says whether a client's at could be when a watch happened: 1970 or before is a clock
// never set.
func atValid(at *time.Time) bool {
	return at == nil || at.After(time.Unix(0, 0)) && !at.After(time.Now().Add(clockSkew))
}

// progressJSON and watchedJSON say when, for a watch sent after the fact: played offline, say.
// No at is now.
type progressJSON struct {
	PositionMS int64      `json:"position_ms"`
	At         *time.Time `json:"at,omitzero"`
}

type watchedJSON struct {
	At *time.Time `json:"at,omitzero"`
}

// progress records where the profile stopped a film or episode, and answers how far that got.
func (a *API) progress(w http.ResponseWriter, r *http.Request) {
	var req progressJSON
	if !a.decode(w, r, &req) {
		return
	}
	if req.PositionMS < 0 || !atValid(req.At) {
		writeProblem(w, a.logger, codeInvalidBody, "position_ms is not negative and "+atRule)
		return
	}
	id, ok := a.pathID(w, r, "id")
	if !ok {
		return
	}
	reach, err := a.svc.Watching.SaveProgress(r.Context(), sessionOf(r).Profile.ID, id, time.Duration(req.PositionMS)*time.Millisecond, domain.ReachStart, req.At)
	if !a.answered(w, r, err) {
		a.titleStateChanged(r, id)
		writeJSON(w, a.logger, "application/json", http.StatusOK, reachedJSON{Reach: reach})
	}
}

// watched marks a title watched, at the time the body says or now for no body.
func (a *API) watched(w http.ResponseWriter, r *http.Request) {
	var req watchedJSON
	if r.ContentLength != 0 && !a.decode(w, r, &req) {
		return
	}
	if !atValid(req.At) {
		writeProblem(w, a.logger, codeInvalidBody, atRule)
		return
	}
	a.mark(func(s watching, ctx context.Context, profile, item uuid.UUID) error {
		return s.MarkWatched(ctx, profile, item, req.At)
	})(w, r)
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
