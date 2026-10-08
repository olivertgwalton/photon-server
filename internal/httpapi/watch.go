package httpapi

import (
	"context"
	"net/http"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/auth"
	"github.com/olivertgwalton/photon-server/internal/domain"
)

type watching interface {
	Length(ctx context.Context, item uuid.UUID) (time.Duration, error)
	SaveProgress(ctx context.Context, profile, item uuid.UUID, position, length time.Duration, before domain.Reach, at *time.Time) (domain.Reach, error)
	MarkWatched(ctx context.Context, profile, item uuid.UUID, at *time.Time) error
	MarkUnwatched(ctx context.Context, profile, item uuid.UUID) error
	ClearProgress(ctx context.Context, profile, item uuid.UUID) error
	Favourite(ctx context.Context, profile, item uuid.UUID) error
	Unfavourite(ctx context.Context, profile, item uuid.UUID) error
	Watchlist(ctx context.Context, profile, item uuid.UUID) error
	Unwatchlist(ctx context.Context, profile, item uuid.UUID) error
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
	length, err := a.svc.Watching.Length(r.Context(), id)
	if a.answered(w, r, err) {
		return
	}
	reach, err := a.svc.Watching.SaveProgress(r.Context(), auth.SessionOf(r.Context()).Profile.ID, id, time.Duration(req.PositionMS)*time.Millisecond, length, domain.ReachStart, req.At)
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
	profile := auth.SessionOf(r.Context()).Profile.ID
	a.svc.Events.Raise(r.Context(), domain.Event{Kind: domain.EventUserDataChanged, Profile: profile, Item: id})
}

// playlistChanged tells the profile's other devices one of its playlists changed.
func (a *API) playlistChanged(r *http.Request, id uuid.UUID) {
	profile := auth.SessionOf(r.Context()).Profile.ID
	a.svc.Events.Raise(r.Context(), domain.Event{
		Kind: domain.EventUserDataChanged, Profile: profile, Details: domain.UserDataDetails{PlaylistID: id},
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
		if !a.answered(w, r, set(a.svc.Watching, r.Context(), auth.SessionOf(r.Context()).Profile.ID, id)) {
			a.titleStateChanged(r, id)
			w.WriteHeader(http.StatusNoContent)
		}
	}
}

func (a *API) watchRoutes() []route {
	return []route{
		{
			pattern: "PUT /api/v1/titles/{id}/progress", access: signedIn,
			summary: "Record where the profile stopped a film or episode it watched outside a playback, a download's say, and when: progress from before the title's state last changed is refused as a conflict",
			body:    progressJSON{}, status: http.StatusOK, reply: reachedJSON{}, handle: a.progress,
		},
		{
			pattern: "DELETE /api/v1/titles/{id}/progress", access: signedIn,
			summary: "Remove a title, or a show's or season's episodes, from Continue Watching, keeping what was watched",
			status:  http.StatusNoContent, handle: a.mark(watching.ClearProgress),
		},
		{
			pattern: "PUT /api/v1/titles/{id}/watched", access: signedIn,
			summary: "Mark a title watched, and when: each film or episode whose state changed since is left as it is",
			body:    optionalBody{watchedJSON{}}, status: http.StatusNoContent, handle: a.watched,
		},
		{
			pattern: "DELETE /api/v1/titles/{id}/watched", access: signedIn, summary: "Mark a title unwatched",
			status: http.StatusNoContent, handle: a.mark(watching.MarkUnwatched),
		},
		{
			pattern: "PUT /api/v1/titles/{id}/favourite", access: signedIn, summary: "Make a title a favourite",
			status: http.StatusNoContent, handle: a.mark(watching.Favourite),
		},
		{
			pattern: "DELETE /api/v1/titles/{id}/favourite", access: signedIn, summary: "Take a title from the favourites",
			status: http.StatusNoContent, handle: a.mark(watching.Unfavourite),
		},
		{
			pattern: "PUT /api/v1/titles/{id}/watchlist", access: signedIn,
			summary: "Put a film or show on the watchlist, a season or episode its show; watching a film, or every episode of a show, takes it off",
			status:  http.StatusNoContent, handle: a.mark(watching.Watchlist),
		},
		{
			pattern: "DELETE /api/v1/titles/{id}/watchlist", access: signedIn,
			summary: "Take a film or show from the watchlist, a season or episode its show",
			status:  http.StatusNoContent, handle: a.mark(watching.Unwatchlist),
		},
	}
}
