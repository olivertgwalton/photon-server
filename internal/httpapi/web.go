package httpapi

import (
	"bytes"
	"cmp"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"regexp"
	"strings"
	"time"
)

// Web is the web app's build (web/, adapter-static), served for every path the API does not own,
// as Jellyfin and Plex serve their web clients.
type Web struct {
	files fs.FS
	// index is the app's one page, with the policy that lets its own script run.
	index []byte
	csp   string
}

// inlineScript is the script SvelteKit writes into index.html to start the app.
var inlineScript = regexp.MustCompile(`(?s)<script>(.*?)</script>`)

// NewWeb reads the build in files, which must hold its index.html.
func NewWeb(files fs.FS) (*Web, error) {
	index, err := fs.ReadFile(files, "index.html")
	if err != nil {
		return nil, fmt.Errorf("the web app's build: %w", err)
	}
	// Everything the app fetches comes from this origin. hls.js plays from blob: media sources and
	// runs its worker from a blob:. Vite inlines small fonts as data:. The page's own inline script
	// is allowed by its hash.
	scripts := []string{"'self'"}
	for _, m := range inlineScript.FindAllSubmatch(index, -1) {
		sum := sha256.Sum256(m[1])
		scripts = append(scripts, "'sha256-"+base64.StdEncoding.EncodeToString(sum[:])+"'")
	}
	csp := strings.Join([]string{
		"default-src 'self'", "script-src " + strings.Join(scripts, " "), "worker-src 'self' blob:",
		"style-src 'self' 'unsafe-inline'", "img-src 'self' data: " + candidatePosters, "media-src 'self' blob:",
		"font-src 'self' data:", "connect-src 'self'", "object-src 'none'", "base-uri 'self'",
		"form-action 'self'", "frame-ancestors 'none'",
	}, "; ")
	return &Web{files: files, index: index, csp: csp}, nil
}

// candidatePosters are the hosts a match candidate's poster is drawn from straight, as the
// built-in providers give it, to tell like-named titles apart by; a plugin's from elsewhere is not.
const candidatePosters = "https://image.tmdb.org https://artworks.thetvdb.com"

// ownedByAPI is whether a path is the API's, answered by it even when it has no such route.
func ownedByAPI(p string) bool {
	return p == "/api" || strings.HasPrefix(p, "/api/") || p == "/readyz"
}

// serveWeb answers a file of the build as it is, and any other path without an extension with the
// app's page, whose script routes it in the browser.
func (a *API) serveWeb(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		writeProblem(w, a.logger, codeMethodNotAllowed, "")
		return
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if name == "" || name == "index.html" {
		a.svc.Web.servePage(w, r)
		return
	}
	switch info, err := fs.Stat(a.svc.Web.files, name); {
	case err == nil && info.Mode().IsRegular():
		if err := a.svc.Web.serveFile(w, r, name); err != nil {
			a.internal(w, r, err)
		}
	case path.Ext(name) == "":
		a.svc.Web.servePage(w, r)
	default:
		writeProblem(w, a.logger, codeNotFound, "")
	}
}

func (web *Web) servePage(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	// A new build names new files, so the page that names them is always asked for again.
	h.Set("Cache-Control", "no-cache")
	h.Set("Content-Security-Policy", web.csp)
	http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(web.index))
}

// encodings are the compressed copies adapter-static's precompress writes beside each file, best
// first.
var encodings = []struct{ token, ext string }{{"br", ".br"}, {"gzip", ".gz"}}

func (web *Web) serveFile(w http.ResponseWriter, r *http.Request, name string) error {
	h := w.Header()
	if strings.HasPrefix(name, "_app/immutable/") {
		// Their names change with their contents.
		h.Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		h.Set("Cache-Control", "no-cache")
	}
	h.Set("Content-Type", cmp.Or(mime.TypeByExtension(path.Ext(name)), "application/octet-stream"))
	h.Add("Vary", "Accept-Encoding")
	f, err := web.open(w, r, name)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	content, ok := f.(io.ReadSeeker)
	if !ok {
		return fmt.Errorf("%s cannot seek", name)
	}
	http.ServeContent(w, r, name, info.ModTime(), content)
	return nil
}

// accepts is whether the client takes the content coding token.
// ponytail: Accept-Encoding is matched by name, not weighed by q; no client refuses br or gzip by q=0.
func accepts(r *http.Request, token string) bool {
	return strings.Contains(r.Header.Get("Accept-Encoding"), token)
}

// open opens the best compressed copy the browser takes, saying which, else the file itself.
func (web *Web) open(w http.ResponseWriter, r *http.Request, name string) (fs.File, error) {
	for _, e := range encodings {
		if !accepts(r, e.token) {
			continue
		}
		if f, err := web.files.Open(name + e.ext); err == nil {
			w.Header().Set("Content-Encoding", e.token)
			return f, nil
		}
	}
	return web.files.Open(name)
}
