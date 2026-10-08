package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/hls"
	"github.com/olivertgwalton/photon-server/internal/playback"
)

// owners say which node of the cluster serves a playback's HLS.
type owners interface {
	Owner(ctx context.Context, playback uuid.UUID) (string, bool, error)
}

type hlsFiles interface {
	Has(playback uuid.UUID) bool
	Resource(ctx context.Context, playback uuid.UUID, name string) (hls.Resource, error)
	Transcodes() (active, conversions, limit int)
	WebVTT(ctx context.Context, open func() (*os.File, error), language string) (string, error)
	Extracted(ctx context.Context, src hls.SubtitleSource, want string) (string, error)
}

func hlsSubject(playback uuid.UUID) string { return "/api/v1/hls/" + playback.String() }

// hlsFile serves a remux's playlist, a part's initialisation or a segment, made as they are asked
// for. The playlist addresses everything else relative to itself, so one signature covers it all.
func (a *API) hlsFile(w http.ResponseWriter, r *http.Request) {
	playback, ok := a.pathID(w, r, "playback")
	if !ok {
		return
	}
	name := r.PathValue("file")
	res, err := a.svc.HLS.Resource(r.Context(), playback, name)
	if a.answered(w, r, err) {
		return
	}
	w.Header().Set("Content-Type", res.Type)
	if res.File == nil {
		_, _ = io.WriteString(w, res.Text)
		return
	}
	a.serveFile(w, r, res.File, name, nil)
}

// routeToOwner hands a request about the playback the path names by param that another node of
// the cluster runs to that node, which checks the request again: its HLS, whose signature every
// node makes with the server's one key, or its player's or an admin's stop of it, so its stream
// ends and its transcode slot is free at once.
func (a *API) routeToOwner(param string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		playback, err := uuid.Parse(r.PathValue(param))
		if err != nil || a.svc.Owners == nil || a.svc.HLS.Has(playback) {
			next.ServeHTTP(w, r)
			return
		}
		address, elsewhere, err := a.svc.Owners.Owner(r.Context(), playback)
		if err != nil {
			a.internal(w, r, err)
			return
		}
		target, perr := url.Parse(address)
		if !elsewhere || perr != nil {
			next.ServeHTTP(w, r)
			return
		}
		a.proxy(w, r, target)
	})
}

// proxy hands a request to another node of the cluster.
func (a *API) proxy(w http.ResponseWriter, r *http.Request, target *url.URL) {
	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			pr.SetXForwarded()
		},
		ErrorLog: slog.NewLogLogger(a.logger.Handler(), slog.LevelWarn),
	}
	proxy.ServeHTTP(w, r)
}

// requireSignedPath admits a request whose path carries a signature of its HLS playback, as
// /api/v1/hls/{playback}/{exp}/{sig}/…, that has not lapsed.
func (a *API) requireSignedPath(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		subject := "/api/v1/hls/" + r.PathValue("playback")
		if !a.svc.Signer.Valid(subject, r.PathValue("exp"), r.PathValue("sig"), time.Now()) {
			writeProblem(w, a.logger, codeUnauthenticated, "the address is not signed, or has lapsed")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *API) hlsRoutes() []route {
	return []route{
		{
			pattern: "GET /api/v1/hls/{playback}/{exp}/{sig}/{file}", access: signedPath,
			summary: "A remux's playlist, initialisation, segment or subtitle segment, at the address play answered",
			path: []param{
				{"exp", "", "When the address lapses, as the server signed it."},
				{"sig", "", "The server's signature of the playback and exp."},
				{"file", "", "main.m3u8, and what it names."},
			},
			status: http.StatusOK, reply: asFile{"application/vnd.apple.mpegurl", "text/vtt", "video/mp4", "video/iso.segment"},
			delivery: playback.DeliverySegment, handle: a.hlsFile,
		},
	}
}
