package httpapi

import (
	"errors"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/peer"
)

// requireHTTPS sends a plain request to its HTTPS address at PHOTON_PUBLIC_URL while secure
// connections are required, as Plex's are, but one from this machine, which may be a health check.
// A trusted proxy's forwarded HTTPS counts as HTTPS. Where the server has no HTTPS address of its
// own the request is refused: the Host it was sent to is the client's word, not the server's.
func (a *API) requireHTTPS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.svc.Secure.Mode() != domain.SecureRequired || a.svc.TrustedProxies.HTTPS(r) || peer.Direct(r).IsLoopback() {
			next.ServeHTTP(w, r)
			return
		}
		public := a.svc.Setup.PublicURL
		if public == nil || public.Scheme != "https" {
			writeProblem(w, a.logger, codeForbidden, "this server takes secure connections only: reach it over https")
			return
		}
		http.Redirect(w, r, onPublicURL(public, r.URL), http.StatusTemporaryRedirect)
	})
}

// onPublicURL is the address of what u asks for at public, its path and query escaped a part at a
// time into public's.
func onPublicURL(public, u *url.URL) string {
	var path, query strings.Builder
	for _, s := range strings.Split(u.Path, "/")[1:] {
		path.WriteString("/" + url.PathEscape(s))
	}
	q := u.Query()
	for _, k := range slices.Sorted(maps.Keys(q)) {
		for _, v := range q[k] {
			if query.Len() > 0 {
				query.WriteByte('&')
			}
			query.WriteString(url.QueryEscape(k) + "=" + url.QueryEscape(v))
		}
	}
	target := public.JoinPath(path.String())
	target.RawQuery = query.String()
	return target.String()
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
