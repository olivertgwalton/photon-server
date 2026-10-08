package jellyfin

import (
	"context"
	"net/http"
	"time"
	"uuid"
)

// userDataChange is Jellyfin's UpdateUserItemDataDto, as much of it as photon keeps: a field left
// out stays as it is. A title's plays are counted as it is played, and its LastPlayedDate is when
// the server is told: an app's date from before the title last changed would have the change
// refused.
type userDataChange struct {
	Played                *bool  `json:"Played"`
	PlaybackPositionTicks *int64 `json:"PlaybackPositionTicks"`
	IsFavorite            *bool  `json:"IsFavorite"`
}

// changeUserData sets what an app says a profile has made of a title: watched or not, then where
// it stopped, then whether it is a favourite.
func (a *API) changeUserData(w http.ResponseWriter, r *http.Request) {
	var c userDataChange
	if !a.readJSON(w, r, &c) {
		return
	}
	if c.PlaybackPositionTicks != nil && *c.PlaybackPositionTicks < 0 {
		a.refuse(w, http.StatusBadRequest)
		return
	}
	a.mark(func(ctx context.Context, profile, item uuid.UUID) error {
		var err error
		switch {
		case c.Played == nil:
		case *c.Played:
			err = a.svc.Watching.MarkWatched(ctx, profile, item, nil)
		default:
			err = a.svc.Watching.MarkUnwatched(ctx, profile, item)
		}
		if err == nil && c.PlaybackPositionTicks != nil {
			if *c.PlaybackPositionTicks == 0 {
				err = a.svc.Watching.ClearProgress(ctx, profile, item)
			} else {
				err = a.saveProgress(ctx, profile, item.String(), time.Duration(*c.PlaybackPositionTicks/ticksPerMS)*time.Millisecond)
			}
		}
		if err == nil && c.IsFavorite != nil {
			if *c.IsFavorite {
				err = a.svc.Watching.Favourite(ctx, profile, item)
			} else {
				err = a.svc.Watching.Unfavourite(ctx, profile, item)
			}
		}
		return err
	})(w, r)
}
