package jellyfin

import (
	"net/http"
	"net/url"
	"strings"
)

// app is what a Jellyfin app says of itself, and the token it holds.
type app struct {
	Client, Device, DeviceID, Version, Token string
}

// appOf reads the Authorization header, `MediaBrowser Client="…", Device="…", DeviceId="…",
// Version="…", Token="…"`, and the ApiKey query parameter where the header holds no token: the
// two places Jellyfin 12 looks by default. Any other scheme says nothing.
func appOf(r *http.Request) app {
	var a app
	if scheme, params, ok := strings.Cut(r.Header.Get("Authorization"), " "); ok && strings.EqualFold(scheme, "MediaBrowser") {
		a = parseParams(params)
	}
	if strings.TrimSpace(a.Token) == "" {
		a.Token = query(r, "ApiKey")
	}
	return a
}

// parseParams is Jellyfin's AuthorizationContext.GetParts, which every Jellyfin app's header is
// written for: a `"` opens or closes a value, a `,` outside one ends it, and a value loses its
// quotes but not its spaces before it is URL-decoded. Keys are case-sensitive, and a later one
// wins.
func parseParams(s string) app {
	var a app
	quoted, start, key := false, 0, ""
	for i := 0; i <= len(s); i++ {
		switch {
		case i < len(s) && s[i] == '"':
			quoted = !quoted
		case i < len(s) && s[i] == '=' && !quoted:
			key, start = strings.TrimSpace(s[start:i]), i+1
		case i == len(s) || (s[i] == ',' && !quoted):
			if start < i {
				a.set(key, s[start:i])
				key = ""
			}
			start = i + 1
		}
	}
	return a
}

func (a *app) set(key, raw string) {
	v := strings.Trim(raw, `"`)
	// As .NET's WebUtility.UrlDecode, which leaves what is not an escape as it is.
	if decoded, err := url.QueryUnescape(v); err == nil {
		v = decoded
	}
	switch key {
	case "Client":
		a.Client = v
	case "Device":
		a.Device = v
	case "DeviceId":
		a.DeviceID = v
	case "Version":
		a.Version = v
	case "Token":
		a.Token = v
	}
}

// query is a query parameter by name, its case ignored as ASP.NET ignores it.
func query(r *http.Request, name string) string {
	for k, v := range r.URL.Query() {
		if strings.EqualFold(k, name) && len(v) > 0 {
			return v[0]
		}
	}
	return ""
}
