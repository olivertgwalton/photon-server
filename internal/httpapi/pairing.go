package httpapi

import (
	"errors"
	"net/http"

	"github.com/olivertgwalton/photon-server/internal/auth"
	"github.com/olivertgwalton/photon-server/internal/kv"
)

type deviceJSON struct {
	Device string `json:"device"`
	Client string `json:"client"`
}

type pairingStartJSON struct {
	DeviceCode     string `json:"device_code"`
	UserCode       string `json:"user_code"`
	PollIntervalMS int64  `json:"poll_interval_ms"`
	ExpiresInMS    int64  `json:"expires_in_ms"`
}

func (a *API) startPairing(w http.ResponseWriter, r *http.Request) {
	var req deviceJSON
	if !a.decode(w, r, &req) {
		return
	}
	if req.Device == "" || req.Client == "" {
		writeProblem(w, a.logger, codeInvalidBody, "device and client are required")
		return
	}
	start, err := a.svc.Auth.StartPairing(r.Context(), auth.Device{Name: req.Device, Client: req.Client})
	if err != nil {
		a.internal(w, r, err)
		return
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, pairingStartJSON{
		DeviceCode: start.DeviceCode, UserCode: start.UserCode,
		PollIntervalMS: auth.PollInterval.Milliseconds(), ExpiresInMS: start.ExpiresIn.Milliseconds(),
	})
}

func (a *API) approvePairing(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserCode string `json:"user_code"`
	}
	if !a.decode(w, r, &req) {
		return
	}
	d, err := a.svc.Auth.ApprovePairing(r.Context(), sessionOf(r), req.UserCode)
	switch {
	case errors.Is(err, auth.ErrPairingNotFound):
		writeProblem(w, a.logger, codePairingNotFound, "")
		return
	case err != nil:
		a.internal(w, r, err)
		return
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, deviceJSON{Device: d.Name, Client: d.Client})
}

func (a *API) pollPairing(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DeviceCode string `json:"device_code"`
	}
	if !a.decode(w, r, &req) {
		return
	}
	state, token, profile, err := a.svc.Auth.PollPairing(r.Context(), req.DeviceCode)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	switch state {
	case kv.PairingApproved:
		writeJSON(w, a.logger, "application/json", http.StatusOK, loginResponse{Token: token, Profile: profileOf(profile)})
	case kv.PairingPending:
		writeProblem(w, a.logger, codeAuthorizationPending, "")
	case kv.PairingSlowDown:
		writeProblem(w, a.logger, codeSlowDown, "")
	case kv.PairingExpired:
		writeProblem(w, a.logger, codeExpiredToken, "")
	}
}
