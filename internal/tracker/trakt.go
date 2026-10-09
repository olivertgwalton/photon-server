package tracker

import (
	"context"
	"errors"
	"net/http"
	"net/url"

	"github.com/olivertgwalton/photon-server/internal/kv"
	"github.com/olivertgwalton/photon-server/internal/provider"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// trakt is Trakt's API, and the host it signs accounts in on, which takes every OAuth request.
type trakt struct {
	api, auth provider.Client
	version   string
}

func newTrakt(version string) trakt {
	return trakt{
		api:     provider.Client{Name: "trakt", Base: "https://api.trakt.tv"},
		auth:    provider.Client{Name: "trakt", Base: "https://auth.trakt.tv"},
		version: version,
	}
}

func (c trakt) header(clientID string) http.Header {
	h := http.Header{}
	h.Set("User-Agent", appName+"/"+c.version)
	h.Set("trakt-api-key", clientID)
	h.Set("trakt-api-version", "2")
	return h
}

func (c trakt) code(ctx context.Context, clientID string) (kv.TrackerLink, error) {
	var body deviceCode
	err := c.auth.Do(ctx, provider.Request{
		Method: http.MethodPost, Path: "/oauth/device/code", Header: c.header(clientID),
		Body: map[string]string{"client_id": clientID},
	}, &body)
	link := body.link()
	// Trakt answers no address with the code in, but fills the code in from its address's path.
	link.VerificationURIComplete = link.VerificationURI + "/" + url.PathEscape(link.UserCode)
	return link, err
}

func (c trakt) token(ctx context.Context, clientID, deviceCode string) (store.TrackerTokens, error) {
	var body grant
	err := c.auth.Do(ctx, provider.Request{
		Method: http.MethodPost, Path: "/oauth/device/token", Header: c.header(clientID),
		Body: map[string]string{"code": deviceCode, "client_id": clientID},
	}, &body)
	if refusal, ok := errors.AsType[*provider.Refusal](err); ok {
		switch refusal.Code {
		case http.StatusBadRequest:
			return store.TrackerTokens{}, errPending
		case http.StatusTooManyRequests:
			return store.TrackerTokens{}, errSlowDown
		// Unknown, used already, expired, and denied.
		case http.StatusNotFound, http.StatusConflict, http.StatusGone, http.StatusTeapot:
			return store.TrackerTokens{}, errGone
		}
	}
	return body.tokens(), err
}

func (c trakt) username(ctx context.Context, clientID, access string) (string, error) {
	h := c.header(clientID)
	h.Set("Authorization", "Bearer "+access)
	var body struct {
		User struct {
			Username string `json:"username"`
		} `json:"user"`
	}
	err := c.api.Do(ctx, provider.Request{Method: http.MethodGet, Path: "/users/settings", Header: h}, &body)
	return body.User.Username, err
}

func (c trakt) revoke(ctx context.Context, clientID string, tok store.TrackerTokens) error {
	_, err := c.auth.Bytes(ctx, provider.Request{
		Method: http.MethodPost, Path: "/oauth/revoke", Header: c.header(clientID),
		Body: map[string]string{"token": tok.Access, "client_id": clientID},
	})
	return err
}
