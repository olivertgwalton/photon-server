package httpapi

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"hash/fnv"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"regexp"
	"strings"

	"github.com/olivertgwalton/photon-server/internal/gzipped"
)

// Web is the web app's build (web/, adapter-static), served for every path the API does not own,
// as Jellyfin and Plex serve their web clients.
type Web struct {
	files fs.FS
	// tag is the app's one page's ETag, the same whichever version of it is sent.
	tag     string
	scripts string
	// objectOrigin is where artwork and previews are read from as the page is served, or "".
	objectOrigin func() string
	// streamOrigins are the nodes' public and local URLs, which a playback another node delivers is
	// fetched from.
	streamOrigins func(ctx context.Context) []string
}

// inlineScript is the script SvelteKit writes into index.html to start the app.
var inlineScript = regexp.MustCompile(`(?s)<script>(.*?)</script>`)

// NewWeb reads the build in files, which must hold its index.html. Its pages may show pictures and
// play sounds from where objectOrigin says clients read artwork and previews as each is served,
// and play streams from the origins streamOrigins says.
func NewWeb(files fs.FS, objectOrigin func() string, streamOrigins func(ctx context.Context) []string) (*Web, error) {
	index, err := fs.ReadFile(files, "index.html")
	if err != nil {
		return nil, fmt.Errorf("the web app's build: %w", err)
	}
	// Everything the app fetches comes from this origin. hls.js plays from blob: media sources and
	// runs its worker from a blob:. JASSUB compiles libass's WebAssembly, which 'wasm-unsafe-eval'
	// allows without allowing eval. Vite inlines small fonts as data:. The page's own inline script
	// is allowed by its hash. Google's Cast sender, which casts to a Chromecast, is loaded from
	// Google, as its guide loads it, and its paths alone are allowed.
	scripts := []string{"'self'", "'wasm-unsafe-eval'", castSender}
	for _, m := range inlineScript.FindAllSubmatch(index, -1) {
		sum := sha256.Sum256(m[1])
		scripts = append(scripts, "'sha256-"+base64.StdEncoding.EncodeToString(sum[:])+"'")
	}
	sum := fnv.New64a()
	// A hash's Write never fails.
	sum.Write(index)
	tag := fmt.Sprintf(`W/"%016x"`, sum.Sum64())
	return &Web{files: files, tag: tag, scripts: strings.Join(scripts, " "), objectOrigin: objectOrigin, streamOrigins: streamOrigins}, nil
}

// csp is the page's policy. A browser holds a redirect's end to it too, so artwork and previews
// sent to their bucket are drawn only while its origin is allowed. A stream is fetched from a
// node's public URL by hls.js, and played from it where the browser plays HLS itself.
func (web *Web) csp(ctx context.Context) string {
	images, media, connect := "img-src 'self' data: "+candidatePosters, "media-src 'self' blob:", "connect-src 'self'"
	if o := web.objectOrigin(); o != "" {
		images, media = images+" "+o, media+" "+o
	}
	for _, o := range web.streamOrigins(ctx) {
		media, connect = media+" "+o, connect+" "+o
	}
	return strings.Join([]string{
		"default-src 'self'", "script-src " + web.scripts, "worker-src 'self' blob:",
		"style-src 'self' 'unsafe-inline'", images, media,
		"font-src 'self' data:", connect, "object-src 'none'", "base-uri 'self'",
		"form-action 'self'", "frame-ancestors 'none'",
	}, "; ")
}

// castSender is where the Cast sender's loader, its framework, and Chrome on Android's own sender
// are fetched from.
const castSender = "https://www.gstatic.com/cv/js/sender/ https://www.gstatic.com/cast/sdk/libs/sender/ " +
	"https://www.gstatic.com/eureka/clank/"

// candidatePosters are the hosts a match candidate's poster is drawn from straight, as the
// built-in providers give it, to tell like-named titles apart by; a plugin's from elsewhere is not.
const candidatePosters = "https://image.tmdb.org https://artworks.thetvdb.com"

// ownedByAPI is whether a path is the API's, answered by it even when it has no such route.
func ownedByAPI(p string) bool {
	return p == "/api" || strings.HasPrefix(p, "/api/") || p == "/health"
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
	var err error
	if name == "" || name == "index.html" {
		err = a.svc.Web.servePage(w, r)
	} else {
		switch info, statErr := fs.Stat(a.svc.Web.files, name); {
		case statErr == nil && info.Mode().IsRegular():
			err = a.svc.Web.serveFile(w, r, name)
		case path.Ext(name) == "":
			err = a.svc.Web.servePage(w, r)
		default:
			writeProblem(w, a.logger, codeNotFound, "")
		}
	}
	if err != nil {
		a.internal(w, r, err)
	}
}

// servePage serves the app's one page, compressed as its other files are. A new build names new
// files, so the page that names them is asked for again each time, and answered 304 while it is
// the same build.
func (web *Web) servePage(w http.ResponseWriter, r *http.Request) error {
	w.Header().Set("Content-Security-Policy", web.csp(r.Context()))
	w.Header().Set("ETag", web.tag)
	return web.serveFile(w, r, "index.html")
}

// encodings are the compressed copies adapter-static's precompress writes beside each file, best
// first.
var encodings = []struct{ token, ext string }{{"br", ".br"}, {"gzip", ".gz"}}

// webTypes are the types of the files a build is made of, fixed: mime.TypeByExtension reads the
// system's table, and a Windows registry that has .js as text/plain, as some installs leave it,
// would have nosniff stop the app.
var webTypes = map[string]string{
	".css": "text/css; charset=utf-8", ".html": "text/html; charset=utf-8", ".js": "text/javascript; charset=utf-8",
	".json": "application/json", ".svg": "image/svg+xml", ".wasm": "application/wasm", ".woff2": "font/woff2",
}

func (web *Web) serveFile(w http.ResponseWriter, r *http.Request, name string) error {
	h := w.Header()
	if strings.HasPrefix(name, "_app/immutable/") {
		// Their names change with their contents.
		h.Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		h.Set("Cache-Control", "no-cache")
	}
	h.Set("Content-Type", cmp.Or(webTypes[path.Ext(name)], mime.TypeByExtension(path.Ext(name)), "application/octet-stream"))
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

// open opens the best compressed version the browser takes, saying which, else the file itself.
func (web *Web) open(w http.ResponseWriter, r *http.Request, name string) (fs.File, error) {
	for _, e := range encodings {
		if !gzipped.Accepts(r, e.token) {
			continue
		}
		if f, err := web.files.Open(name + e.ext); err == nil {
			w.Header().Set("Content-Encoding", e.token)
			return f, nil
		}
	}
	return web.files.Open(name)
}
