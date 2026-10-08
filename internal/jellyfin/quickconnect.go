package jellyfin

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/auth"
	"github.com/olivertgwalton/photon-server/internal/kv"
)

// Quick Connect is Jellyfin's name for photon's pairing: an app shows a code, a signed-in app
// approves it, and the first, asking after its secret, signs in.

// quickConnectResult is Jellyfin's QuickConnectResult. Its Secret is photon's device code. A
// pairing keeps no DeviceId or version, so those are the asking app's own.
type quickConnectResult struct {
	Authenticated bool      `json:"Authenticated"`
	Secret        string    `json:"Secret"`
	Code          string    `json:"Code"`
	DeviceID      string    `json:"DeviceId"`
	DeviceName    string    `json:"DeviceName"`
	AppName       string    `json:"AppName"`
	AppVersion    string    `json:"AppVersion"`
	DateAdded     time.Time `json:"DateAdded"`
}

func (a *API) initiateQuickConnect(w http.ResponseWriter, r *http.Request) {
	app := appOf(r)
	if app.Client == "" || app.Device == "" || app.DeviceID == "" || app.Version == "" {
		a.refuse(w, http.StatusBadRequest)
		return
	}
	if !a.allowed(w, r, auth.PairingsPerAddress, auth.PairingKey(a.svc.Proxies.Client(r))) {
		return
	}
	start, err := a.svc.Auth.StartPairing(r.Context(), auth.Device{Name: app.Device, Client: app.Client}, auth.CodeDigits)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	a.writeJSON(w, quickConnectResult{
		Secret: start.DeviceCode, Code: start.UserCode, DeviceID: app.DeviceID, DeviceName: app.Device,
		AppName: app.Client, AppVersion: app.Version, DateAdded: time.Now().UTC(),
	})
}

// quickConnectState answers an app asking, every few seconds, whether its code is approved. It
// leaves the pairing for authenticateWithQuickConnect to take.
func (a *API) quickConnectState(w http.ResponseWriter, r *http.Request) {
	secret := query(r, "secret")
	state, p, err := a.svc.Auth.PairingStatus(r.Context(), secret)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	switch state {
	case kv.PairingPending, kv.PairingApproved:
	case kv.PairingExpired, kv.PairingSlowDown:
		a.refuse(w, http.StatusNotFound)
		return
	}
	app := appOf(r)
	a.writeJSON(w, quickConnectResult{
		Authenticated: state == kv.PairingApproved, Secret: secret, Code: p.UserCode, DeviceID: app.DeviceID,
		DeviceName: p.Device.Name, AppName: p.Device.Client, AppVersion: app.Version, DateAdded: p.Started.UTC(),
	})
}

// authorizeQuickConnect approves a code for the signed-in profile, and no other.
func (a *API) authorizeQuickConnect(w http.ResponseWriter, r *http.Request) {
	s := sessionOf(r)
	if userID := query(r, "userId"); userID != "" {
		if id, err := uuid.Parse(userID); err != nil || id != s.Profile.ID {
			a.refuse(w, http.StatusForbidden)
			return
		}
	}
	if !a.allowed(w, r, auth.ApprovalsPerProfile, auth.ApprovalKey(s.Profile.ID)) {
		return
	}
	_, err := a.svc.Auth.ApprovePairing(r.Context(), s, query(r, "code"))
	switch {
	case errors.Is(err, auth.ErrPairingNotFound):
		a.refuse(w, http.StatusNotFound)
		return
	case err != nil:
		a.internal(w, r, err)
		return
	}
	a.writeJSON(w, true)
}

// authenticateWithQuickConnect signs in the app holding an approved secret, once.
func (a *API) authenticateWithQuickConnect(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Secret string `json:"Secret"`
	}
	a.readWithin(w)
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxSignIn)).Decode(&req); err != nil {
		a.refuse(w, http.StatusBadRequest)
		return
	}
	state, token, profile, err := a.svc.Auth.PollPairing(r.Context(), req.Secret)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	switch state {
	case kv.PairingApproved:
	case kv.PairingPending, kv.PairingSlowDown, kv.PairingExpired:
		a.refuse(w, http.StatusNotFound)
		return
	}
	a.writeJSON(w, authenticationResult{User: a.userOf(profile), AccessToken: token, ServerID: a.id})
}
