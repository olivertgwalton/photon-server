package httpapi

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"reflect"
	"strings"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/auth"
	"github.com/olivertgwalton/photon-server/internal/domain"
)

type sessionKey struct{}

func sessionOf(r *http.Request) domain.Session {
	s, _ := r.Context().Value(sessionKey{}).(domain.Session)
	return s
}

// sessionCookie is where the web app keeps a browser's token (web/src/lib/server/session.ts), so
// the browser calls the API itself.
const sessionCookie = "photon_session"

// crossOrigin refuses a browser's write from another site. Only a cookie needs it: a page on
// another site can make a browser send its cookie, never a bearer token.
var crossOrigin = http.NewCrossOriginProtection()

// requireSession admits a request carrying a valid device token in its Authorization header, or in
// the web app's cookie: never from the query, where it would land in logs.
func (a *API) requireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok {
			cookie, err := r.Cookie(sessionCookie)
			if err != nil {
				a.unauthenticated(w)
				return
			}
			if err := crossOrigin.Check(r); err != nil {
				writeProblem(w, a.logger, codeForbidden, "a write from another site")
				return
			}
			token = cookie.Value
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

// decode reads a JSON body into v, refusing an unknown field as an unknown query parameter is, and
// a value of an enum none of its values.
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
	if err == nil {
		err = checkEnums(reflect.ValueOf(v), false)
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
	// Avatar is the profile's picture, at /api/v1/artwork/{id}.
	Avatar uuid.UUID `json:"avatar,omitzero"`
}

func profileOf(p domain.Profile) profileJSON {
	return profileJSON{ID: p.ID.String(), Name: p.Name, Role: p.Role, Avatar: p.Avatar}
}

type loginRequest struct {
	Name     string `json:"name"`
	Password string `json:"password"`
	Device   string `json:"device"`
	Client   string `json:"client"`
	// Keep is token when left out.
	Keep domain.Keep `json:"keep,omitzero"`
}

// loginResponse has no token for a session kept in the cookie.
type loginResponse struct {
	Token   string      `json:"token,omitzero"`
	Profile profileJSON `json:"profile"`
}

// sessionCookieAge is as long as a browser keeps any cookie: a device's session does not lapse on
// its own.
const sessionCookieAge = 400 * 24 * time.Hour

// keepSession sets the cookie, or with no token clears it, Secure when the browser reached the
// server over HTTPS. A server on a home network is often reached over plain HTTP by its address,
// where a Secure cookie would never be stored.
func (a *API) keepSession(w http.ResponseWriter, r *http.Request, token string) {
	maxAge := int(sessionCookieAge.Seconds())
	if token == "" {
		maxAge = -1
	}
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // Secure wherever the browser has HTTPS, as said above
		Name: sessionCookie, Value: token, Path: "/", MaxAge: maxAge,
		HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: a.svc.TrustedProxies.HTTPS(r),
	})
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
	req.Keep = cmp.Or(req.Keep, domain.KeepToken)
	if !a.allowed(w, r, signInsPerAddress, a.addrKey(r, "signin")) ||
		!a.allowed(w, r, signInsPerName, nameKey("signin", req.Name)) {
		return
	}
	token, profile, err := a.svc.Auth.SignIn(r.Context(), req.Name, req.Password, auth.Device{Name: req.Device, Client: req.Client})
	// Who tried, from where and on what, as Jellyfin logs a sign-in; never the password.
	details := map[string]any{
		"name": req.Name, "device": req.Device, "client": req.Client,
		"address": a.svc.TrustedProxies.Client(r).String(),
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
	switch req.Keep {
	case domain.KeepToken:
		writeJSON(w, a.logger, "application/json", http.StatusOK, loginResponse{Token: token, Profile: profileOf(profile)})
	case domain.KeepCookie:
		a.keepSession(w, r, token)
		writeJSON(w, a.logger, "application/json", http.StatusOK, loginResponse{Profile: profileOf(profile)})
	}
}

func (a *API) logout(w http.ResponseWriter, r *http.Request) {
	if err := a.svc.Auth.SignOut(r.Context(), sessionOf(r).ID); err != nil {
		a.internal(w, r, err)
		return
	}
	if _, err := r.Cookie(sessionCookie); err == nil {
		a.keepSession(w, r, "")
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) me(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, a.logger, "application/json", http.StatusOK, profileOf(sessionOf(r).Profile))
}
