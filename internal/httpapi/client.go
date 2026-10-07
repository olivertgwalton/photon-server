package httpapi

import (
	"errors"
	"net/http"
	"net/url"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/peer"
)

// requireHTTPS sends a plain request to its HTTPS address while secure connections are required,
// as Plex's are, but one from this machine, which may be a health check. A trusted proxy's
// forwarded HTTPS counts as HTTPS.
func (a *API) requireHTTPS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.svc.Secure.Mode() != domain.SecureRequired || a.svc.TrustedProxies.HTTPS(r) || peer.Direct(r).IsLoopback() {
			next.ServeHTTP(w, r)
			return
		}
		//nolint:gosec // to the host the client already reached, as it named it; nowhere else
		http.Redirect(w, r, "https://"+r.Host+r.URL.RequestURI(), http.StatusTemporaryRedirect)
	})
}

// ParsePublicURL reads PHOTON_PUBLIC_URL, the http or https address readers reach the server's
// web app at; nil where it is not set.
func ParsePublicURL(s string) (*url.URL, error) {
	if s == "" {
		return nil, nil
	}
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("PHOTON_PUBLIC_URL is not an http or https address with a host, and no credentials, query or fragment")
	}
	return u, nil
}

// publicURL is where a reader reaches the web app: PHOTON_PUBLIC_URL, else the address this
// request came to, over HTTPS where it came that way.
func (a *API) publicURL(r *http.Request) *url.URL {
	if u := a.svc.Setup.PublicURL; u != nil {
		return u
	}
	scheme := "http"
	if a.svc.TrustedProxies.HTTPS(r) {
		scheme = "https"
	}
	return &url.URL{Scheme: scheme, Host: r.Host}
}
