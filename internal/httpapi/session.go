package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

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

const maxBody = 64 << 10

// decode reads a JSON body into v, refusing an unknown field as an unknown query parameter is.
func (a *API) decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeProblem(w, a.logger, codeInvalidBody, err.Error())
		return false
	}
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
	switch {
	case errors.Is(err, auth.ErrInvalidCredentials):
		writeProblem(w, a.logger, codeInvalidCredentials, "")
		return
	case err != nil:
		a.internal(w, r, err)
		return
	}
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
