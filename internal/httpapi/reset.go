package httpapi

import (
	"log/slog"
	"net/http"

	"github.com/olivertgwalton/photon-server/internal/auth"
)

// A forgotten password is reset as Jellyfin resets one: asked for from the server's local network,
// by a profile's name, its code is written where only the server's operator reads it, here this
// node's log, and redeemed with a new password. Unlike Jellyfin's PIN, a code is never the
// password, and guessing one is limited as signing in is.

type resetJSON struct {
	Name string `json:"name"`
}

// resetStartJSON says where to find the code: in the log of node, for expires_in_ms. It is the
// same whether or not a profile has the name.
type resetStartJSON struct {
	Node        string `json:"node"`
	ExpiresInMS int64  `json:"expires_in_ms"`
}

type redemptionJSON struct {
	Code     string `json:"code"`
	Password string `json:"password"`
}

func (a *API) startReset(w http.ResponseWriter, r *http.Request) {
	var req resetJSON
	if !a.decode(w, r, &req) {
		return
	}
	if req.Name == "" {
		writeProblem(w, a.logger, codeInvalidBody, "name is required")
		return
	}
	if !a.allowed(w, r, auth.ResetsPerAddress, a.addrKey(r, "reset")) {
		return
	}
	local, err := a.local(r)
	if a.answered(w, r, err) {
		return
	}
	if !local {
		writeProblem(w, a.logger, codeForbidden, "a password is reset from the server's local network")
		return
	}
	code, err := a.svc.Auth.StartReset(r.Context(), req.Name)
	if a.answered(w, r, err) {
		return
	}
	address := a.svc.Reach.Client(r).String()
	if code == "" {
		a.logger.WarnContext(r.Context(), "a password reset was asked for a name no profile has", slog.String("name", req.Name), slog.String("address", address))
	} else {
		a.logger.WarnContext(r.Context(), "a password reset was asked for; give its code to the profile's owner alone",
			slog.String("name", req.Name), slog.String("code", code), slog.String("address", address), slog.Duration("expires_in", auth.ResetTTL))
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, resetStartJSON{Node: a.svc.Placer.Self().Name, ExpiresInMS: auth.ResetTTL.Milliseconds()})
}

func (a *API) redeemReset(w http.ResponseWriter, r *http.Request) {
	var req redemptionJSON
	if !a.decode(w, r, &req) {
		return
	}
	if req.Code == "" {
		writeProblem(w, a.logger, codeInvalidBody, "code is required")
		return
	}
	if !a.allowed(w, r, auth.ResetsPerAddress, a.addrKey(r, "redeem")) {
		return
	}
	profile, err := a.svc.Auth.RedeemReset(r.Context(), req.Code, req.Password)
	if a.answered(w, r, err) {
		return
	}
	a.logger.WarnContext(r.Context(), "a password was reset", slog.String("profile", profile.String()), slog.String("address", a.svc.Reach.Client(r).String()))
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) resetRoutes() []route {
	return []route{
		{
			pattern: "POST /api/v1/auth/password-resets", access: public,
			summary: "Ask, from the server's local network, to reset a profile's forgotten password; its code is written to the server's log",
			body:    resetJSON{}, status: http.StatusOK, reply: resetStartJSON{}, handle: a.startReset,
		},
		{
			pattern: "POST /api/v1/auth/password-resets/redemptions", access: public,
			summary: "Set a new password by a reset's code, signing out the profile's every device",
			body:    redemptionJSON{}, status: http.StatusNoContent, handle: a.redeemReset,
		},
	}
}
