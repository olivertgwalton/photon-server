package httpapi

import (
	"cmp"
	"net/http"

	"github.com/olivertgwalton/photon-server/internal/auth"
	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// setupState is whether a server is still to be set up by making its first admin, and whether the
// client asking may. Setting up is open while the server has no profile, so it cannot open again
// once it has one, and, as Plex's claiming is, only to a client on the server's local networks:
// whoever reaches a new server first from anywhere else could otherwise claim it.
type setupState string

const (
	setupDone setupState = "done"
	setupOpen setupState = "open"
	// setupLocalOnly is a server to be set up by a client on its local networks, not this one.
	setupLocalOnly setupState = "local_only"
)

func setupStates() []setupState {
	return []setupState{setupDone, setupOpen, setupLocalOnly}
}

type setupJSON struct {
	State setupState `json:"state"`
}

// setupRequest names the first admin and its password, and the device setting up, which is
// signed in as it, kept as a sign-in keeps it.
type setupRequest struct {
	Name     string      `json:"name"`
	Password string      `json:"password"`
	Device   string      `json:"device"`
	Client   string      `json:"client"`
	Keep     domain.Keep `json:"keep,omitzero"`
}

func (a *API) setupState(r *http.Request) (setupState, error) {
	has, err := a.svc.Profiles.HasProfiles(r.Context())
	if err != nil || has {
		return setupDone, err
	}
	local, err := a.local(r)
	if err != nil || !local {
		return setupLocalOnly, err
	}
	return setupOpen, nil
}

func (a *API) setup(w http.ResponseWriter, r *http.Request) {
	state, err := a.setupState(r)
	if !a.answered(w, r, err) {
		writeJSON(w, a.logger, "application/json", http.StatusOK, setupJSON{State: state})
	}
}

func (a *API) setUp(w http.ResponseWriter, r *http.Request) {
	var req setupRequest
	if !a.decode(w, r, &req) {
		return
	}
	name, ok := domain.ProfileName(req.Name)
	if !ok || req.Device == "" || req.Client == "" {
		writeProblem(w, a.logger, codeInvalidBody, badName+", and device and client are required")
		return
	}
	if !a.allowed(w, r, auth.SignInsPerAddress, a.addrKey(r, "setup")) {
		return
	}
	// Asked before the password is hashed, which a server set up never needs.
	state, err := a.setupState(r)
	if a.answered(w, r, err) {
		return
	}
	switch state {
	case setupDone:
		a.answered(w, r, store.ErrSetUp)
		return
	case setupLocalOnly:
		writeProblem(w, a.logger, codeForbidden, "a new server is set up from its local network")
		return
	case setupOpen:
	}
	token, profile, err := a.svc.Auth.SetUp(r.Context(), name, req.Password, auth.Device{Name: req.Device, Client: req.Client})
	if a.answered(w, r, err) {
		return
	}
	a.svc.Events.Raise(r.Context(), domain.Event{Kind: domain.EventProfileAdded, Profile: profile.ID, Details: domain.NameDetails{Name: profile.Name}})
	a.signedIn(w, r, cmp.Or(req.Keep, domain.KeepToken), token, profile)
}

func (a *API) setupRoutes() []route {
	return []route{
		{
			pattern: "GET /api/v1/setup", access: public,
			summary: "Say whether the server is still to be set up, and whether this client may set it up",
			status:  http.StatusOK, reply: setupJSON{}, handle: a.setup,
		},
		{
			pattern: "POST /api/v1/setup", access: public,
			summary: "Set a new server up from its local network: add its first admin and sign this device in as it",
			body:    setupRequest{}, status: http.StatusOK, reply: loginResponse{}, handle: a.setUp,
		},
	}
}
