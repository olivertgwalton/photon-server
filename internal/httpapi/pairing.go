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

func (a *API) startPairing(w http.ResponseWriter, r *http.Request) {
	var req deviceJSON
	if !a.decode(w, r, &req) {
		return
	}
	if req.Device == "" || req.Client == "" {
		writeProblem(w, a.logger, codeInvalidBody, "device and client are required")
		return
	}
	if !a.allowed(w, r, auth.PairingsPerAddress, auth.PairingKey(a.svc.TrustedProxies.Client(r))) {
		return
	}
	start, err := a.svc.Auth.StartPairing(r.Context(), auth.Device{Name: req.Device, Client: req.Client}, auth.CodeLetters)
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
	if !a.allowed(w, r, auth.ApprovalsPerProfile, auth.ApprovalKey(sessionOf(r).Profile.ID)) {
		return
	}
	d, err := a.svc.Auth.ApprovePairing(r.Context(), sessionOf(r), req.UserCode)
	if a.answered(w, r, err) {
		return
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, deviceJSON{Device: d.Name, Client: d.Client})
}

// signInByPairing signs in the device a pairing started for, once a signed-in device approved it;
// until then the problem says why not, as RFC 8628's token endpoint does.
func (a *API) signInByPairing(w http.ResponseWriter, r *http.Request, req loginRequest) {
	if req.DeviceCode == "" {
		writeProblem(w, a.logger, codeInvalidBody, "device_code is required")
		return
	}
	state, token, profile, err := a.svc.Auth.PollPairing(r.Context(), req.DeviceCode)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	switch state {
	case kv.PairingApproved:
		a.signedIn(w, r, req.Keep, token, profile)
	case kv.PairingPending:
		writeProblem(w, a.logger, codeAuthorizationPending, "")
	case kv.PairingSlowDown:
		writeProblem(w, a.logger, codeSlowDown, "")
	case kv.PairingExpired:
		writeProblem(w, a.logger, codeExpiredToken, "")
	}
}

func (a *API) pairingRoutes() []route {
	return []route{
		{
			pattern: "POST /api/v1/auth/pairings", access: public,
			summary: "Start pairing a device by a code shown on it (RFC 8628); it signs in by the pairing once approved",
			body:    deviceJSON{}, status: http.StatusOK, reply: pairingStartJSON{}, handle: a.startPairing,
		},
		{
			pattern: "POST /api/v1/auth/pairings/approve", access: signedIn,
			summary: "Approve a pairing by its code, signing that device in as this profile",
			body:    approvalJSON{}, status: http.StatusOK, reply: deviceJSON{}, handle: a.approvePairing,
		},
	}
}
