package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/olivertgwalton/photon-server/internal/auth"
	"github.com/olivertgwalton/photon-server/internal/domain"
)

type sessionKey struct{}

func sessionOf(r *http.Request) domain.Session {
	s, _ := r.Context().Value(sessionKey{}).(domain.Session)
	return s
}

// requireSession admits a request carrying a valid device token in its Authorization header, the
// only place a token is read: never from the query, where it would land in logs.
func (a *API) requireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok {
			a.unauthenticated(w)
			return
		}
		session, err := a.svc.Auth.Authenticate(r.Context(), token)
		switch {
		case errors.Is(err, auth.ErrUnauthenticated):
			a.unauthenticated(w)
			return
		case err != nil:
			a.internal(w, r, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), sessionKey{}, session)))
	})
}

func (a *API) unauthenticated(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", "Bearer")
	writeProblem(w, a.logger, codeUnauthenticated, "")
}

func (a *API) internal(w http.ResponseWriter, r *http.Request, err error) {
	a.logger.ErrorContext(r.Context(), "request failed", slog.String("route", r.Pattern), slog.Any("err", err))
	writeProblem(w, a.logger, codeInternal, "")
}

const (
	maxBody = 64 << 10
	// bodyWithin is how long a client has to send a body, so one sending it a byte at a time, as
	// anyone may to sign in, does not hold its request open for good.
	bodyWithin = 10 * time.Second
)

// decode reads a JSON body into v, refusing an unknown field as an unknown query parameter is.
func (a *API) decode(w http.ResponseWriter, r *http.Request, v any) bool {
	rc := http.NewResponseController(w)
	// Not every ResponseWriter has a connection to time: a test's recorder has none.
	_ = rc.SetReadDeadline(time.Now().Add(bodyWithin))
	body := http.MaxBytesReader(w, r.Body, maxBody)
	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()
	err := dec.Decode(v)
	if err == nil {
		// What follows the value is read within the deadline too, not by the server once the
		// handler is done.
		_, err = io.Copy(io.Discard, body)
	}
	if err != nil {
		// The deadline stands, so what is left of the body is read within it or the connection
		// closed.
		writeProblem(w, a.logger, codeInvalidBody, err.Error())
		return false
	}
	// A deadline left set would end the request's context as it passed, as a server's ReadTimeout
	// does, and a handler may outlast it.
	_ = rc.SetReadDeadline(time.Time{})
	return true
}

type profileJSON struct {
	ID   string      `json:"id"`
	Name string      `json:"name"`
	Role domain.Role `json:"role"`
}

func profileOf(p domain.Profile) profileJSON {
	return profileJSON{ID: p.ID.String(), Name: p.Name, Role: p.Role}
}

type loginRequest struct {
	Name     string `json:"name"`
	Password string `json:"password"`
	Device   string `json:"device"`
	Client   string `json:"client"`
}

type loginResponse struct {
	Token   string      `json:"token"`
	Profile profileJSON `json:"profile"`
}

func (a *API) login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if !a.decode(w, r, &req) {
		return
	}
	if req.Name == "" || req.Device == "" || req.Client == "" {
		writeProblem(w, a.logger, codeInvalidBody, "name, device and client are required")
		return
	}
	if !a.allowed(w, r, signInsPerAddress, a.addrKey(r, "signin")) ||
		!a.allowed(w, r, signInsPerName, nameKey("signin", req.Name)) {
		return
	}
	token, profile, err := a.svc.Auth.SignIn(r.Context(), req.Name, req.Password, auth.Device{Name: req.Device, Client: req.Client})
	// Who tried, from where and on what, as Jellyfin logs a sign-in; never the password.
	details := map[string]any{
		"name": req.Name, "device": req.Device, "client": req.Client,
		"address": clientAddr(r, a.svc.TrustedProxies).String(),
	}
	switch {
	case errors.Is(err, auth.ErrInvalidCredentials):
		a.svc.Events.Raise(r.Context(), domain.Event{Kind: domain.EventSignInRefused, Details: details})
		writeProblem(w, a.logger, codeInvalidCredentials, "")
		return
	case err != nil:
		a.internal(w, r, err)
		return
	}
	a.svc.Events.Raise(r.Context(), domain.Event{Kind: domain.EventSignedIn, Profile: profile.ID, Details: details})
	writeJSON(w, a.logger, "application/json", http.StatusOK, loginResponse{Token: token, Profile: profileOf(profile)})
}

func (a *API) logout(w http.ResponseWriter, r *http.Request) {
	if err := a.svc.Auth.SignOut(r.Context(), sessionOf(r).ID); err != nil {
		a.internal(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) me(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, a.logger, "application/json", http.StatusOK, profileOf(sessionOf(r).Profile))
}
