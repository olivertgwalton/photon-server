package httpapi

import (
	"net/http"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/auth"
)

type authenticatorJSON struct {
	AddedAt time.Time `json:"added_at"`
}

// authenticatorChangeJSON gives the profile an authenticator app. Secret is the key the app was
// given, in base32 as an otpauth:// address carries it, of 160 bits or more; code is the app's
// now, showing it holds the key.
type authenticatorChangeJSON struct {
	Secret string `json:"secret"`
	Code   string `json:"code"`
}

func (a *API) ownAuthenticator(w http.ResponseWriter, r *http.Request) {
	a.writeAuthenticator(w, r, auth.SessionOf(r.Context()).Profile.ID, nil)
}

func (a *API) profileAuthenticator(w http.ResponseWriter, r *http.Request) {
	if id, ok := a.pathID(w, r, "id"); ok {
		a.writeAuthenticator(w, r, id, keeper(r))
	}
}

func (a *API) writeAuthenticator(w http.ResponseWriter, r *http.Request, profile uuid.UUID, by *uuid.UUID) {
	added, err := a.svc.Auth.Authenticator(r.Context(), profile, by)
	if !a.answeredAs(w, r, err, "the profile has no authenticator app") {
		writeJSON(w, a.logger, "application/json", http.StatusOK, authenticatorJSON{AddedAt: added.UTC()})
	}
}

func (a *API) setAuthenticator(w http.ResponseWriter, r *http.Request) {
	var req authenticatorChangeJSON
	if !a.decode(w, r, &req) {
		return
	}
	if req.Secret == "" || req.Code == "" {
		writeProblem(w, a.logger, codeInvalidBody, "secret and code are required")
		return
	}
	if !a.answered(w, r, a.svc.Auth.SetAuthenticator(r.Context(), auth.SessionOf(r.Context()).Profile.ID, req.Secret, req.Code)) {
		w.WriteHeader(http.StatusNoContent)
	}
}

func (a *API) removeOwnAuthenticator(w http.ResponseWriter, r *http.Request) {
	a.removeAuthenticator(w, r, auth.SessionOf(r.Context()).Profile.ID, nil)
}

func (a *API) removeProfileAuthenticator(w http.ResponseWriter, r *http.Request) {
	if id, ok := a.pathID(w, r, "id"); ok {
		a.removeAuthenticator(w, r, id, keeper(r))
	}
}

func (a *API) removeAuthenticator(w http.ResponseWriter, r *http.Request, profile uuid.UUID, by *uuid.UUID) {
	if !a.answeredAs(w, r, a.svc.Auth.RemoveAuthenticator(r.Context(), profile, by), "the profile has no authenticator app") {
		w.WriteHeader(http.StatusNoContent)
	}
}

func (a *API) authenticatorRoutes() []route {
	return []route{
		{
			pattern: "GET /api/v1/profile/authenticator", access: signedIn,
			summary: "The authenticator app whose code signing in by the profile's password also asks for",
			status:  http.StatusOK, reply: authenticatorJSON{}, handle: a.ownAuthenticator,
		},
		{
			pattern: "PUT /api/v1/profile/authenticator", access: signedIn,
			summary: "Give the profile an authenticator app, in place of any before, by the key it holds and its code now",
			body:    authenticatorChangeJSON{}, status: http.StatusNoContent, handle: a.setAuthenticator,
		},
		{
			pattern: "DELETE /api/v1/profile/authenticator", access: signedIn,
			summary: "Take the profile's authenticator app away, so its password alone signs it in",
			status:  http.StatusNoContent, handle: a.removeOwnAuthenticator,
		},
		{
			pattern: "GET /api/v1/admin/profiles/{id}/authenticator", access: manages,
			summary: "Any profile's authenticator app",
			status:  http.StatusOK, reply: authenticatorJSON{}, handle: a.profileAuthenticator,
		},
		{
			pattern: "DELETE /api/v1/admin/profiles/{id}/authenticator", access: manages,
			summary: "Take any profile's authenticator app away, for one whose phone is lost",
			status:  http.StatusNoContent, handle: a.removeProfileAuthenticator,
		},
	}
}
