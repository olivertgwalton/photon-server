package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

const startScript = `kit.start(app, element);`

func webAPI(t *testing.T) *API {
	t.Helper()
	return webAPIFrom(t, func() string { return "" })
}

// webAPIFrom serves a build whose pages may draw from where objectOrigin says as each is served.
func webAPIFrom(t *testing.T, objectOrigin func() string) *API {
	t.Helper()
	web, err := NewWeb(fstest.MapFS{
		"index.html":                     {Data: []byte(`<!doctype html><script>` + startScript + `</script>`)},
		"favicon.svg":                    {Data: []byte(`<svg/>`)},
		"_app/immutable/entry/app.js":    {Data: []byte(`export {}`)},
		"_app/immutable/entry/app.js.br": {Data: []byte(`brotli`)},
	}, objectOrigin)
	if err != nil {
		t.Fatal(err)
	}
	return New(slog.New(slog.DiscardHandler), domain.Info{}, Services{
		Ready: func(context.Context) error { return nil }, Auth: fakeAuth{}, Limits: &fakeLimiter{}, Web: web,
	})
}

func get(a *API, target string, header ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, target, nil)
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	rec := httptest.NewRecorder()
	a.ServeHTTP(rec, req)
	return rec
}

func TestEveryPageOfTheAppIsItsIndex(t *testing.T) {
	a := webAPI(t)
	for _, path := range []string{"/", "/titles/0199", "/auth/login", "/index.html"} {
		rec := get(a, path)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), startScript) {
			t.Errorf("%s: status %d, body %q", path, rec.Code, rec.Body)
		}
		if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
			t.Errorf("%s: Cache-Control %q", path, got)
		}
	}
}

func TestThePageMayRunOnlyItsOwnScript(t *testing.T) {
	csp := get(webAPI(t), "/").Header().Get("Content-Security-Policy")
	sum := sha256.Sum256([]byte(startScript))
	// WebAssembly is compiled, for the player's styled subtitles, but no script is evaluated.
	if !strings.Contains(csp, "script-src 'self' 'wasm-unsafe-eval' 'sha256-"+base64.StdEncoding.EncodeToString(sum[:])+"'") ||
		strings.Contains(csp, "'unsafe-eval'") || !strings.Contains(csp, "frame-ancestors 'none'") {
		t.Errorf("Content-Security-Policy %q", csp)
	}
	// Pictures come from the server, but for match candidates' posters from the built-in
	// providers' own hosts, and from nowhere else.
	if !strings.Contains(csp, "img-src 'self' data: https://image.tmdb.org https://artworks.thetvdb.com;") {
		t.Errorf("Content-Security-Policy %q, want images from the server and the providers' hosts alone", csp)
	}
}

// Artwork and previews sent to their bucket are drawn, and theme tunes played, from its origin,
// whichever it is as the page is served.
func TestThePageMayDrawFromTheBucketClientsAreSentTo(t *testing.T) {
	origin := "https://media.example.com"
	api := webAPIFrom(t, func() string { return origin })
	csp := get(api, "/").Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "img-src 'self' data: https://image.tmdb.org https://artworks.thetvdb.com https://media.example.com;") ||
		!strings.Contains(csp, "media-src 'self' blob: https://media.example.com;") {
		t.Errorf("Content-Security-Policy %q, want pictures and sounds from the bucket's origin", csp)
	}
	origin = ""
	if csp := get(api, "/").Header().Get("Content-Security-Policy"); strings.Contains(csp, "media.example.com") {
		t.Errorf("Content-Security-Policy %q once clients are sent nowhere, want the bucket gone from it", csp)
	}
}

func TestTheAppsFilesAreServedAsTheyAre(t *testing.T) {
	a := webAPI(t)
	rec := get(a, "/favicon.svg")
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/svg+xml" || rec.Body.String() != `<svg/>` {
		t.Errorf("favicon: %d %q %q", rec.Code, rec.Header().Get("Content-Type"), rec.Body)
	}
	if rec.Header().Get("Cache-Control") != "no-cache" {
		t.Errorf("favicon Cache-Control %q", rec.Header().Get("Cache-Control"))
	}

	rec = get(a, "/_app/immutable/entry/app.js")
	if rec.Body.String() != `export {}` || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/javascript") {
		t.Errorf("script: %q %q", rec.Header().Get("Content-Type"), rec.Body)
	}
	if got := rec.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Errorf("script Cache-Control %q", got)
	}

	rec = get(a, "/_app/immutable/entry/app.js", "Accept-Encoding", "gzip, deflate, br")
	if rec.Body.String() != "brotli" || rec.Header().Get("Content-Encoding") != "br" ||
		!strings.HasPrefix(rec.Header().Get("Content-Type"), "text/javascript") {
		t.Errorf("brotli: %q %q %q", rec.Header().Get("Content-Encoding"), rec.Header().Get("Content-Type"), rec.Body)
	}
}

func TestAMissingFileIsNotThePage(t *testing.T) {
	if rec := get(webAPI(t), "/_app/immutable/gone.js"); rec.Code != http.StatusNotFound {
		t.Errorf("status %d, want 404", rec.Code)
	}
}

func TestTheAPIsPathsStayTheAPIs(t *testing.T) {
	a := webAPI(t)
	for _, path := range []string{"/api/v1/nothing", "/api", "/readyz"} {
		rec := get(a, path)
		if rec.Header().Get("Content-Type") == "text/html; charset=utf-8" {
			t.Errorf("%s answered the web app", path)
		}
	}
	if rec := get(a, "/api/v1/server"); rec.Code != http.StatusOK {
		t.Errorf("server: %d", rec.Code)
	}
}

func TestTheAppIsOnlyRead(t *testing.T) {
	rec := httptest.NewRecorder()
	webAPI(t).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/auth/login", nil))
	if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != "GET, HEAD" {
		t.Errorf("status %d, Allow %q", rec.Code, rec.Header().Get("Allow"))
	}
}

func TestNoBuildNoWebApp(t *testing.T) {
	if _, err := NewWeb(fstest.MapFS{}, func() string { return "" }); err == nil {
		t.Error("a build with no index.html was taken")
	}
}
