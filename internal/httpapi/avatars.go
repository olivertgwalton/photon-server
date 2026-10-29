package httpapi

import (
	"context"
	"net/http"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/auth"
	"github.com/olivertgwalton/photon-server/internal/domain"
)

type avatars interface {
	SetAvatar(ctx context.Context, profile, picture uuid.UUID, by *uuid.UUID) (domain.Profile, error)
}

// setOwnAvatar gives the profile a picture, as Jellyfin's user image is set from its profile page.
func (a *API) setOwnAvatar(w http.ResponseWriter, r *http.Request) {
	a.setAvatar(w, r, auth.SessionOf(r.Context()).Profile.ID, nil)
}

func (a *API) clearOwnAvatar(w http.ResponseWriter, r *http.Request) {
	a.clearAvatar(w, r, auth.SessionOf(r.Context()).Profile.ID, nil)
}

// setProfileAvatar is an admin giving any profile its picture.
func (a *API) setProfileAvatar(w http.ResponseWriter, r *http.Request) {
	if id, ok := a.pathID(w, r, "id"); ok {
		a.setAvatar(w, r, id, keeper(r))
	}
}

func (a *API) clearProfileAvatar(w http.ResponseWriter, r *http.Request) {
	if id, ok := a.pathID(w, r, "id"); ok {
		a.clearAvatar(w, r, id, keeper(r))
	}
}

// setAvatar keeps the picture in the body and makes it the profile's.
func (a *API) setAvatar(w http.ResponseWriter, r *http.Request, profile uuid.UUID, by *uuid.UUID) {
	picture, _, ok := a.keepPicture(w, r)
	if !ok {
		return
	}
	p, err := a.svc.Avatars.SetAvatar(r.Context(), profile, picture, by)
	if !a.answered(w, r, err) {
		writeJSON(w, a.logger, "application/json", http.StatusOK, profileOf(p))
	}
}

func (a *API) clearAvatar(w http.ResponseWriter, r *http.Request, profile uuid.UUID, by *uuid.UUID) {
	if _, err := a.svc.Avatars.SetAvatar(r.Context(), profile, uuid.UUID{}, by); !a.answered(w, r, err) {
		w.WriteHeader(http.StatusNoContent)
	}
}

func (a *API) avatarsRoutes() []route {
	return []route{
		{
			pattern: "POST /api/v1/profile/avatar", access: signedIn,
			summary: "Give the profile a picture: a JPEG, PNG, GIF or WebP of at most 32 MiB and 50 megapixels",
			body:    pictureTypes, status: http.StatusOK, reply: profileJSON{}, handle: a.setOwnAvatar,
		},
		{
			pattern: "DELETE /api/v1/profile/avatar", access: signedIn, summary: "Take the profile's picture away",
			status: http.StatusNoContent, handle: a.clearOwnAvatar,
		},
		{
			pattern: "POST /api/v1/admin/profiles/{id}/avatar", access: manages,
			summary: "Give any profile a picture: a JPEG, PNG, GIF or WebP of at most 32 MiB and 50 megapixels",
			body:    pictureTypes, status: http.StatusOK, reply: profileJSON{}, handle: a.setProfileAvatar,
		},
		{
			pattern: "DELETE /api/v1/admin/profiles/{id}/avatar", access: manages, summary: "Take any profile's picture away",
			status: http.StatusNoContent, handle: a.clearProfileAvatar,
		},
	}
}
