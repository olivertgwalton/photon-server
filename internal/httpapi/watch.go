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
func (a *API) progress(r *http.Request, req progressJSON) (reachedJSON, error) {
	if req.PositionMS < 0 || !atValid(req.At) {
		return reachedJSON{}, refuse(codeInvalidBody, "position_ms is not negative and "+atRule)
	}
	id := pathUUID(r, "id")
	length, err := a.svc.Watching.Length(r.Context(), id)
	if err != nil {
		return reachedJSON{}, err
	}
	reach, err := a.svc.Watching.SaveProgress(r.Context(), auth.SessionOf(r.Context()).Profile.ID, id, time.Duration(req.PositionMS)*time.Millisecond, length, domain.ReachStart, req.At)
	if err != nil {
		return reachedJSON{}, err
	}
	a.titleStateChanged(r, id)
	return reachedJSON{Reach: reach}, nil
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
	_, err := a.mark(func(s watching, ctx context.Context, profile, item uuid.UUID) error {
		return s.MarkWatched(ctx, profile, item, req.At)
	})(r, none{})
	if !a.answered(w, r, err) {
		w.WriteHeader(http.StatusNoContent)
	}
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
func (a *API) mark(set func(w watching, ctx context.Context, profile, item uuid.UUID) error) func(*http.Request, none) (none, error) {
	return func(r *http.Request, _ none) (none, error) {
		id := pathUUID(r, "id")
		if err := set(a.svc.Watching, r.Context(), auth.SessionOf(r.Context()).Profile.ID, id); err != nil {
			return none{}, err
		}
		a.titleStateChanged(r, id)
		return none{}, nil
	}
}

func (a *API) watchRoutes() []route {
	return []route{
		a.handles(route{
			pattern: "PUT /api/v1/titles/{id}/progress", access: signedIn,
			summary: "Record where the profile stopped a film or episode it watched outside a playback, a download's say, and when: progress from before the title's state last changed is refused as a conflict",
			status:  http.StatusOK,
		}, a.progress),
		a.handles(route{
			pattern: "DELETE /api/v1/titles/{id}/progress", access: signedIn,
			summary: "Remove a title, or a show's or season's episodes, from Continue Watching, keeping what was watched",
			status:  http.StatusNoContent,
		}, a.mark(watching.ClearProgress)),
		{
			pattern: "PUT /api/v1/titles/{id}/watched", access: signedIn,
			summary: "Mark a title watched, and when: each film or episode whose state changed since is left as it is",
			body:    optionalBody{watchedJSON{}}, status: http.StatusNoContent, handle: a.watched,
		},
		a.handles(route{
			pattern: "DELETE /api/v1/titles/{id}/watched", access: signedIn, summary: "Mark a title unwatched",
			status: http.StatusNoContent,
		}, a.mark(watching.MarkUnwatched)),
		a.handles(route{
			pattern: "PUT /api/v1/titles/{id}/favourite", access: signedIn, summary: "Make a title a favourite",
			status: http.StatusNoContent,
		}, a.mark(watching.Favourite)),
		a.handles(route{
			pattern: "DELETE /api/v1/titles/{id}/favourite", access: signedIn, summary: "Take a title from the favourites",
			status: http.StatusNoContent,
		}, a.mark(watching.Unfavourite)),
		a.handles(route{
			pattern: "PUT /api/v1/titles/{id}/watchlist", access: signedIn,
			summary: "Put a film or show on the watchlist, a season or episode its show; watching a film, or every episode of a show, takes it off",
			status:  http.StatusNoContent,
		}, a.mark(watching.Watchlist)),
		a.handles(route{
			pattern: "DELETE /api/v1/titles/{id}/watchlist", access: signedIn,
			summary: "Take a film or show from the watchlist, a season or episode its show",
			status:  http.StatusNoContent,
		}, a.mark(watching.Unwatchlist)),
	}
}
