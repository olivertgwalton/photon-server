package httpapi

import (
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

type switchJSON struct {
	ProfileID string `json:"profile_id"`
	// Secret is the PIN or password the profile's lock asks for.
	Secret string `json:"secret,omitzero"`
}

type pinJSON struct {
	PIN string `json:"pin"`
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
	writeJSON(w, a.logger, "application/json", http.StatusOK, listJSON[profileListingJSON]{Items: out})
}

func (a *API) switchProfile(w http.ResponseWriter, r *http.Request) {
	var req switchJSON
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
	if !a.answered(w, r, err) {
		writeJSON(w, a.logger, "application/json", http.StatusOK, profileOf(profile))
	}
}

func (a *API) setPIN(w http.ResponseWriter, r *http.Request) {
	var req pinJSON
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
	if !a.answered(w, r, a.svc.Auth.SetPIN(r.Context(), sessionOf(r).Profile.ID, pin)) {
		w.WriteHeader(http.StatusNoContent)
	}
}

type passwordChangeJSON struct {
	Current string `json:"current"`
	New     string `json:"new"`
}

// changePassword is a profile changing its own password, as Jellyfin's profile page does. It is
// limited as signing in is, since it answers whether a password is right.
func (a *API) changePassword(w http.ResponseWriter, r *http.Request) {
	var req passwordChangeJSON
	if !a.decode(w, r, &req) {
		return
	}
	session := sessionOf(r)
	if !a.allowed(w, r, auth.SignInsPerAddress, a.addrKey(r, "password")) ||
		!a.allowed(w, r, auth.SignInsPerName, "password:profile:"+session.Profile.ID.String()) {
		return
	}
	if !a.answered(w, r, a.svc.Auth.ChangePassword(r.Context(), session, req.Current, req.New)) {
		w.WriteHeader(http.StatusNoContent)
	}
}

// renameSelf is a profile changing its own name, as a Jellyfin user may. Names are unique.
func (a *API) renameSelf(w http.ResponseWriter, r *http.Request) {
	var req nameJSON
	if !a.decode(w, r, &req) {
		return
	}
	name, ok := domain.ProfileName(req.Name)
	if !ok {
		writeProblem(w, a.logger, codeInvalidBody, badName)
		return
	}
	p, err := a.svc.ProfileAdmin.SetProfile(r.Context(), sessionOf(r).Profile.ID, store.ProfileChange{Name: name}, nil)
	if !a.answered(w, r, err) {
		writeJSON(w, a.logger, "application/json", http.StatusOK, profileOf(p))
	}
}
