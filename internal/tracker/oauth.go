package tracker

import (
	"cmp"
	"encoding/json"
	"time"

	"github.com/olivertgwalton/photon-server/internal/kv"
	"github.com/olivertgwalton/photon-server/internal/provider"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// appName is how the server names itself to a tracker, with its version: Trakt may refuse a
// request with no User-Agent, and Simkl asks every request to name its app.
const appName = "Photon"

// deviceCode is a tracker's answer to a device asking for a code, as RFC 8628 has it, or as Trakt
// names its address.
type deviceCode struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURL         string `json:"verification_url"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

func (d deviceCode) link() kv.TrackerLink {
	return kv.TrackerLink{
		DeviceCode: d.DeviceCode, UserCode: d.UserCode,
		VerificationURI: cmp.Or(d.VerificationURI, d.VerificationURL), VerificationURIComplete: d.VerificationURIComplete,
		Interval: time.Duration(d.Interval) * time.Second,
		Expires:  time.Now().Add(time.Duration(d.ExpiresIn) * time.Second),
	}
}

// grant is what a tracker answers a code entered or a token refreshed with.
type grant struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
}

func (g grant) tokens() store.TrackerTokens {
	return store.TrackerTokens{
		Access: g.AccessToken, Refresh: g.RefreshToken, Expires: time.Now().Add(time.Duration(g.ExpiresIn) * time.Second),
	}
}

// oauthError is the error an OAuth refusal names, as RFC 6749 has it: "invalid_grant", for one.
func oauthError(r *provider.Refusal) string {
	var body struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(r.Body, &body) != nil {
		return ""
	}
	return body.Error
}
