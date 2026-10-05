package httpapi

import (
	"errors"
	"net/http"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/auth"
	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
)

type profileListingJSON struct {
	profileJSON
	Lock domain.ProfileLock `json:"lock"`
}

func (a *API) profiles(w http.ResponseWriter, r *http.Request) {
	list, err := a.svc.Profiles.Profiles(r.Context())
	if err != nil {
		a.internal(w, r, err)
		return
	}
	out := make([]profileListingJSON, len(list))
	for i, p := range list {
		out[i] = profileListingJSON{profileJSON: profileOf(p.Profile), Lock: p.Lock}
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, map[string]any{"items": out})
}

func (a *API) switchProfile(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ProfileID string `json:"profile_id"`
		Secret    string `json:"secret"`
	}
	if !a.decode(w, r, &req) {
		return
	}
	target, err := uuid.Parse(req.ProfileID)
	if err != nil {
		writeProblem(w, a.logger, codeInvalidBody, "profile_id is not an id")
		return
	}
	if !a.allowed(w, r, switchesPerSession, "switch:session:"+sessionOf(r).ID.String()) {
		return
	}
	profile, err := a.svc.Auth.SwitchProfile(r.Context(), sessionOf(r), target, req.Secret)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeProblem(w, a.logger, codeNotFound, "")
	case errors.Is(err, auth.ErrWrongSecret):
		writeProblem(w, a.logger, codeWrongSecret, "")
	case err != nil:
		a.internal(w, r, err)
	default:
		writeJSON(w, a.logger, "application/json", http.StatusOK, profileOf(profile))
	}
}

func (a *API) setPIN(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PIN string `json:"pin"`
	}
	if !a.decode(w, r, &req) {
		return
	}
	if req.PIN == "" {
		writeProblem(w, a.logger, codeInvalidBody, "pin is required; DELETE clears it")
		return
	}
	a.writePIN(w, r, req.PIN)
}

func (a *API) clearPIN(w http.ResponseWriter, r *http.Request) {
	a.writePIN(w, r, "")
}

func (a *API) writePIN(w http.ResponseWriter, r *http.Request, pin string) {
	err := a.svc.Auth.SetPIN(r.Context(), sessionOf(r).Profile.ID, pin)
	switch {
	case errors.Is(err, auth.ErrPINNotDigits):
		writeProblem(w, a.logger, codeInvalidBody, err.Error())
	case err != nil:
		a.internal(w, r, err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}
