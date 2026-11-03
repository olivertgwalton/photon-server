package httpapi

import (
	"net/http"

	"github.com/olivertgwalton/photon-server/internal/auth"
	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
)

type profileListingJSON struct {
	profileJSON
	Lock domain.ProfileLock `json:"lock"`
}

type switchJSON struct {
	// Secret is the PIN or password the profile's lock asks for.
	Secret string `json:"secret,omitzero"`
}

type pinJSON struct {
	PIN string `json:"pin"`
}

func (a *API) profiles(r *http.Request, _ none) (listJSON[profileListingJSON], error) {
	list, err := a.svc.Profiles.Profiles(r.Context())
	if err != nil {
		return listJSON[profileListingJSON]{}, err
	}
	out := make([]profileListingJSON, len(list))
	for i, p := range list {
		out[i] = profileListingJSON{profileJSON: profileOf(p.Profile), Lock: p.Lock}
	}
	return listJSON[profileListingJSON]{Items: out}, nil
}

func (a *API) switchProfile(w http.ResponseWriter, r *http.Request) {
	var req switchJSON
	if r.ContentLength != 0 && !a.decode(w, r, &req) {
		return
	}
	target := pathUUID(r, "id")
	if !a.allowed(w, r, switchesPerSession, "switch:session:"+auth.SessionOf(r.Context()).ID.String()) {
		return
	}
	// A guess at a profile's secret is also counted against that profile, as a sign-in is against
	// a name, or a member minting sessions by approving their own pairings guesses without limit.
	if req.Secret != "" && !a.allowed(w, r, auth.SignInsPerName, "switch:profile:"+target.String()) {
		return
	}
	session := auth.SessionOf(r.Context())
	profile, err := a.svc.Auth.SwitchProfile(r.Context(), session, target, req.Secret)
	if !a.answered(w, r, err) {
		a.svc.Events.Raise(r.Context(), domain.Event{Kind: domain.EventSessionsChanged, Profile: session.Profile.ID})
		writeJSON(w, a.logger, "application/json", http.StatusOK, profileOf(profile))
	}
}

func (a *API) setPIN(r *http.Request, req pinJSON) (none, error) {
	if req.PIN == "" {
		return none{}, refuse(codeInvalidBody, "pin is required; DELETE clears it")
	}
	return none{}, a.svc.Auth.SetPIN(r.Context(), auth.SessionOf(r.Context()).Profile.ID, req.PIN)
}

func (a *API) clearPIN(r *http.Request, _ none) (none, error) {
	return none{}, a.svc.Auth.SetPIN(r.Context(), auth.SessionOf(r.Context()).Profile.ID, "")
}

// passwordChangeJSON has no current password for a profile with none yet: one a sign-in provider's
// account was given.
type passwordChangeJSON struct {
	Current string `json:"current,omitzero"`
	New     string `json:"new"`
}

// changePassword is a profile changing its own password, as Jellyfin's profile page does. It is
// limited as signing in is, since it answers whether a password is right.
func (a *API) changePassword(w http.ResponseWriter, r *http.Request) {
	var req passwordChangeJSON
	if !a.decode(w, r, &req) {
		return
	}
	session := auth.SessionOf(r.Context())
	if !a.allowed(w, r, auth.SignInsPerAddress, a.addrKey(r, "password")) ||
		!a.allowed(w, r, auth.SignInsPerName, "password:profile:"+session.Profile.ID.String()) {
		return
	}
	if !a.answered(w, r, a.svc.Auth.ChangePassword(r.Context(), session, req.Current, req.New)) {
		a.svc.Events.Raise(r.Context(), domain.Event{Kind: domain.EventSessionsChanged, Profile: session.Profile.ID})
		w.WriteHeader(http.StatusNoContent)
	}
}

// renameSelf is a profile changing its own name, as a Jellyfin user may. Names are unique.
func (a *API) renameSelf(r *http.Request, req nameJSON) (profileJSON, error) {
	name, ok := domain.ProfileName(req.Name)
	if !ok {
		return profileJSON{}, refuse(codeInvalidBody, badName)
	}
	p, err := a.svc.ProfileAdmin.SetProfile(r.Context(), auth.SessionOf(r.Context()).Profile.ID, store.ProfileChange{Name: name}, nil)
	if err != nil {
		return profileJSON{}, err
	}
	return profileOf(p), nil
}

func (a *API) profilesRoutes() []route {
	return []route{
		a.handles(route{
			pattern: "GET /api/v1/profiles", access: signedIn, summary: "List the household's profiles and their locks",
			status: http.StatusOK,
		}, a.profiles),
		{
			pattern: "POST /api/v1/profiles/{id}/switch", access: signedIn, summary: "Switch the profile this device watches as",
			body: optionalBody{switchJSON{}}, status: http.StatusOK, reply: profileJSON{}, handle: a.switchProfile,
		},
		a.handles(route{
			pattern: "PATCH /api/v1/profile", access: signedIn,
			summary: "Rename the profile; names are unique, and every device shows the new one at once",
			status:  http.StatusOK,
		}, a.renameSelf),
		a.handles(route{
			pattern: "PUT /api/v1/profile/pin", access: signedIn, summary: "Set the profile's PIN",
			status: http.StatusNoContent,
		}, a.setPIN),
		a.handles(route{
			pattern: "DELETE /api/v1/profile/pin", access: signedIn, summary: "Clear the profile's PIN",
			status: http.StatusNoContent,
		}, a.clearPIN),
		{
			pattern: "PUT /api/v1/profile/password", access: signedIn,
			summary: "Change the profile's password, or set its first, signing out its other devices",
			body:    passwordChangeJSON{}, status: http.StatusNoContent, handle: a.changePassword,
		},
	}
}
