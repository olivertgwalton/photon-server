package httpapi

import (
	"net/http"
	"net/url"

	"github.com/olivertgwalton/photon-server/internal/auth"
	"github.com/olivertgwalton/photon-server/internal/kv"
)

type deviceJSON struct {
	Device string `json:"device"`
	Client string `json:"client"`
}

type pairingStartJSON struct {
	DeviceCode string `json:"device_code"`
	UserCode   string `json:"user_code"`
	// VerificationURI is the web app's page a reader enters the code at, and
	// VerificationURIComplete the same with the code filled in, for a QR code (RFC 8628 3.3.1).
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	PollIntervalMS          int64  `json:"poll_interval_ms"`
	ExpiresInMS             int64  `json:"expires_in_ms"`
}

type approvalJSON struct {
	UserCode string `json:"user_code"`
}

type pollJSON struct {
	DeviceCode string `json:"device_code"`
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
	if !a.allowed(w, r, pairingsPerAddress, a.addrKey(r, "pairing")) {
		return
	}
	start, err := a.svc.Auth.StartPairing(r.Context(), auth.Device{Name: req.Device, Client: req.Client})
	if err != nil {
		a.internal(w, r, err)
		return
	}
	link := a.publicURL(r).JoinPath("link")
	complete := *link
	complete.RawQuery = url.Values{"code": {start.UserCode}}.Encode()
	writeJSON(w, a.logger, "application/json", http.StatusOK, pairingStartJSON{
		DeviceCode: start.DeviceCode, UserCode: start.UserCode,
		VerificationURI: link.String(), VerificationURIComplete: complete.String(),
		PollIntervalMS: auth.PollInterval.Milliseconds(), ExpiresInMS: start.ExpiresIn.Milliseconds(),
	})
}

func (a *API) approvePairing(w http.ResponseWriter, r *http.Request) {
	var req approvalJSON
	if !a.decode(w, r, &req) {
		return
	}
	if !a.allowed(w, r, approvalsPerProfile, "approve:profile:"+sessionOf(r).Profile.ID.String()) {
		return
	}
	d, err := a.svc.Auth.ApprovePairing(r.Context(), sessionOf(r), req.UserCode)
	if a.answered(w, r, err) {
		return
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, deviceJSON{Device: d.Name, Client: d.Client})
}

func (a *API) pollPairing(w http.ResponseWriter, r *http.Request) {
	var req pollJSON
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
