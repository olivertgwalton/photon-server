package tracker

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"

	"github.com/olivertgwalton/photon-server/internal/kv"
	"github.com/olivertgwalton/photon-server/internal/provider"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// simkl is Simkl's API, signing accounts in by its OAuth 2.0 flows, not the PIN flow they replace.
type simkl struct {
	api     provider.Client
	version string
}

func newSimkl(version string) simkl {
	return simkl{api: provider.Client{Name: "simkl", Base: "https://api.simkl.com"}, version: version}
}

// request is r as Simkl takes every request: naming the app in its query as well as its header.
func (c simkl) request(clientID string, r provider.Request) provider.Request {
	r.Query = url.Values{"client_id": {clientID}, "app-name": {appName}, "app-version": {c.version}}
	r.Header = http.Header{}
	r.Header.Set("User-Agent", appName+"/"+c.version)
	return r
}

func (c simkl) code(ctx context.Context, clientID string) (kv.TrackerLink, error) {
	var body deviceCode
	err := c.api.Do(ctx, c.request(clientID, provider.Request{
		Method: http.MethodPost, Path: "/oauth2/device",
		// Without media:write a token reads and never writes, which is what Simkl grants a scope it
		// does not know too.
		Body: map[string]string{"client_id": clientID, "scope": "media:read media:write"},
	}), &body)
	return body.link(), err
}

func (c simkl) token(ctx context.Context, clientID, deviceCode string) (store.TrackerTokens, error) {
	var body grant
	err := c.api.Do(ctx, c.request(clientID, provider.Request{
		Method: http.MethodPost, Path: "/oauth2/token",
		Body: map[string]string{
			"grant_type": "urn:ietf:params:oauth:grant-type:device_code", "client_id": clientID, "device_code": deviceCode,
		},
	}), &body)
	var oauth struct {
		Error string `json:"error"`
	}
	if refusal, ok := errors.AsType[*provider.Refusal](err); ok && refusal.Code == http.StatusBadRequest && json.Unmarshal(refusal.Body, &oauth) == nil {
		// Simkl never says a code was denied: one is pending until it expires.
		switch oauth.Error {
		case "authorization_pending":
			return store.TrackerTokens{}, errPending
		case "slow_down":
			return store.TrackerTokens{}, errSlowDown
		case "expired_token", "invalid_grant", "access_denied":
			return store.TrackerTokens{}, errGone
		}
	}
	return body.tokens(), err
}

func (c simkl) username(ctx context.Context, clientID, access string) (string, error) {
	r := c.request(clientID, provider.Request{Method: http.MethodGet, Path: "/users/settings"})
	r.Header.Set("Authorization", "Bearer "+access)
	var body struct {
		User struct {
			Name string `json:"name"`
		} `json:"user"`
	}
	err := c.api.Do(ctx, r, &body)
	return body.User.Name, err
}

// revoke ends the grant whole, which its refresh token outlives the access token of.
func (c simkl) revoke(ctx context.Context, clientID string, tok store.TrackerTokens) error {
	_, err := c.api.Bytes(ctx, c.request(clientID, provider.Request{
		Method: http.MethodPost, Path: "/oauth2/revoke",
		Body: map[string]string{"client_id": clientID, "token": tok.Refresh},
	}))
	return err
}
