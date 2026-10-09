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

// mdblist is MDBList's API, signing accounts in by OAuth 2.0 at paths that each end in a slash,
// which MDBList requires, and with forms, the only bodies they take.
type mdblist struct {
	api     provider.Client
	version string
}

func newMDBList(version string) mdblist {
	return mdblist{api: provider.Client{Name: "mdblist", Base: "https://api.mdblist.com"}, version: version}
}

// mdblistHistory is the path of each historyWrite on MDBList.
var mdblistHistory = map[historyWrite]string{historyAdd: "/sync/watched", historyRemove: "/sync/watched/remove"}

func (c mdblist) request(access string, r provider.Request) provider.Request {
	r.Header = http.Header{}
	r.Header.Set("User-Agent", appName+"/"+c.version)
	if access != "" {
		r.Header.Set("Authorization", "Bearer "+access)
	}
	return r
}

func (c mdblist) code(ctx context.Context, clientID string) (kv.TrackerLink, error) {
	var body deviceCode
	err := c.api.Do(ctx, c.request("", provider.Request{
		Method: http.MethodPost, Path: "/oauth/device-authorization/",
		// Without write a token reads and never writes.
		Body: url.Values{"client_id": {clientID}, "scope": {"read write"}},
	}), &body)
	// MDBList answers a client id it does not know 400, as a request it cannot read.
	if refusal, ok := errors.AsType[*provider.Refusal](err); ok && refusal.Code == http.StatusBadRequest &&
		(oauthError(refusal) == "invalid_request" || oauthError(refusal) == "invalid_client") {
		return kv.TrackerLink{}, errUnknownClient
	}
	return body.link(), err
}

func (c mdblist) token(ctx context.Context, clientID, deviceCode string) (store.TrackerTokens, error) {
	var body grant
	err := c.api.Do(ctx, c.request("", provider.Request{
		Method: http.MethodPost, Path: "/oauth/token/",
		Body: url.Values{
			"grant_type": {"urn:ietf:params:oauth:grant-type:device_code"}, "client_id": {clientID}, "device_code": {deviceCode},
		},
	}), &body)
	refusal, ok := errors.AsType[*provider.Refusal](err)
	switch {
	case !ok:
	// A code it no longer has: used already, or expired long since.
	case refusal.Code == http.StatusNotFound:
		return store.TrackerTokens{}, errGone
	case refusal.Code == http.StatusBadRequest:
		switch oauthError(refusal) {
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

func (c mdblist) username(ctx context.Context, _, access string) (string, error) {
	var body struct {
		Username string `json:"username"`
	}
	err := c.api.Do(ctx, c.request(access, provider.Request{Method: http.MethodGet, Path: "/user"}), &body)
	return body.Username, err
}

// revoke ends the grant whole: revoking the refresh token revokes its access token too.
func (c mdblist) revoke(ctx context.Context, clientID string, tok store.TrackerTokens) error {
	_, err := c.api.Bytes(ctx, c.request("", provider.Request{
		Method: http.MethodPost, Path: "/oauth/revoke_token/",
		Body: url.Values{"client_id": {clientID}, "token": {tok.Refresh}, "token_type_hint": {"refresh_token"}},
	}))
	return err
}

func (c mdblist) refresh(ctx context.Context, clientID string, old store.TrackerTokens) (store.TrackerTokens, error) {
	var body grant
	err := c.api.Do(ctx, c.request("", provider.Request{
		Method: http.MethodPost, Path: "/oauth/token/",
		Body: url.Values{"grant_type": {"refresh_token"}, "client_id": {clientID}, "refresh_token": {old.Refresh}},
	}), &body)
	// A 401 is the app's client id refused, which is the admin's to set right; the grant stands.
	if refusal, ok := errors.AsType[*provider.Refusal](err); ok && refusal.Code == http.StatusBadRequest && oauthError(refusal) == "invalid_grant" {
		return store.TrackerTokens{}, errGrantGone
	}
	return body.tokens(), err
}

// mdblistPlay is a play as MDBList takes it, an episode as a season of its show.
type mdblistPlay struct {
	Progress float64      `json:"progress"`
	Movie    *titled      `json:"movie,omitzero"`
	Show     *mdblistShow `json:"show,omitzero"`
}

type mdblistShow struct {
	titled
	Season mdblistSeason `json:"season"`
}

type mdblistSeason struct {
	Number  int            `json:"number"`
	Episode mdblistEpisode `json:"episode"`
}

type mdblistEpisode struct {
	Number int `json:"number"`
}

func (c mdblist) scrobble(ctx context.Context, _, access string, a action, p play) error {
	body := mdblistPlay{Progress: p.Progress, Movie: p.Movie}
	if p.Show != nil {
		body.Show = &mdblistShow{titled: *p.Show, Season: mdblistSeason{
			Number: p.Episode.Season, Episode: mdblistEpisode{Number: p.Episode.Number},
		}}
	}
	_, err := c.api.Bytes(ctx, c.request(access, provider.Request{Method: http.MethodPost, Path: "/scrobble/" + string(a), Body: body}))
	return err
}

func (c mdblist) history(ctx context.Context, _, access string, w historyWrite, h history) error {
	_, err := c.api.Bytes(ctx, c.request(access, provider.Request{Method: http.MethodPost, Path: mdblistHistory[w], Body: h}))
	return err
}
