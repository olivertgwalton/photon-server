package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/artwork"
	"github.com/olivertgwalton/photon-server/internal/domain"
)

type avatars interface {
	SetAvatar(ctx context.Context, profile, picture uuid.UUID) (domain.Profile, error)
}

// avatarWithin is how long a client has to send an avatar, the largest picture kept taking a
// minute at 4 Mbit/s.
const avatarWithin = time.Minute

// avatarTypes are what an avatar may be sent as; its bytes, not the header, say which it is.
var avatarTypes = asFile{"image/jpeg", "image/png", "image/gif", "image/webp"}

// setOwnAvatar gives the profile a picture, as Jellyfin's user image is set from its profile page.
func (a *API) setOwnAvatar(w http.ResponseWriter, r *http.Request) {
	a.setAvatar(w, r, sessionOf(r).Profile.ID)
}

func (a *API) clearOwnAvatar(w http.ResponseWriter, r *http.Request) {
	a.clearAvatar(w, r, sessionOf(r).Profile.ID)
}

// setProfileAvatar is an admin giving any profile its picture.
func (a *API) setProfileAvatar(w http.ResponseWriter, r *http.Request) {
	if id, ok := a.pathID(w, r); ok {
		a.setAvatar(w, r, id)
	}
}

func (a *API) clearProfileAvatar(w http.ResponseWriter, r *http.Request) {
	if id, ok := a.pathID(w, r); ok {
		a.clearAvatar(w, r, id)
	}
}

// setAvatar keeps the picture in the body under a new id, so a client that keeps pictures for
// good is shown the new one, and makes it the profile's.
func (a *API) setAvatar(w http.ResponseWriter, r *http.Request, profile uuid.UUID) {
	rc := http.NewResponseController(w)
	// Not every ResponseWriter has a connection to time: a test's recorder has none.
	_ = rc.SetReadDeadline(time.Now().Add(avatarWithin))
	picture := uuid.NewV7()
	err := a.svc.Artwork.Keep(picture, r.Body)
	_ = rc.SetReadDeadline(time.Time{})
	if errors.Is(err, artwork.ErrNotPicture) {
		writeProblem(w, a.logger, codeInvalidBody, err.Error())
		return
	}
	if err != nil {
		a.internal(w, r, err)
		return
	}
	p, err := a.svc.Avatars.SetAvatar(r.Context(), profile, picture)
	if !a.answered(w, r, err) {
		writeJSON(w, a.logger, "application/json", http.StatusOK, profileOf(p))
	}
}

func (a *API) clearAvatar(w http.ResponseWriter, r *http.Request, profile uuid.UUID) {
	if _, err := a.svc.Avatars.SetAvatar(r.Context(), profile, uuid.UUID{}); !a.answered(w, r, err) {
		w.WriteHeader(http.StatusNoContent)
	}
}
