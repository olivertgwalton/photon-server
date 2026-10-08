package httpapi

import (
	"context"
	"log/slog"
	"math"
	"net/http"
	"os"
	"strconv"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/auth"
	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/hls"
	"github.com/olivertgwalton/photon-server/internal/library"
	"github.com/olivertgwalton/photon-server/internal/playback"
)

// partStream serves one file of a copy as it is, in byte ranges.
func (a *API) partStream(w http.ResponseWriter, r *http.Request) {
	a.serveLibraryFile(w, r, "id", a.svc.Playing.PartFile, math.MaxInt64)
}

// playbackPartStream serves a copy's file as it is to its playback, for as long as the playback
// lasts: its stop cuts off what is still being sent.
func (a *API) playbackPartStream(w http.ResponseWriter, r *http.Request) {
	playback, ok := a.pathID(w, r, "id")
	if !ok {
		return
	}
	rc := http.NewResponseController(w)
	ctx := r.Context()
	cut := func() {
		// Failing, the connection is gone already, and so is what was being sent.
		if err := rc.SetWriteDeadline(time.Now()); err != nil {
			a.logger.DebugContext(ctx, "stream not cut", slog.Any("err", err))
		}
	}
	done, err := a.svc.Playbacks.Serve(ctx, playback, cut)
	if a.answered(w, r, err) {
		return
	}
	defer done()
	a.serveLibraryFile(w, r, "part", a.svc.Playing.PartFile, math.MaxInt64)
}

// sampleBytes is as much of a part as a connection test may read: enough to time a fast link,
// too little to stand in for a download.
const sampleBytes = 16 << 20

// partSample serves the start of a part's file to time the connection, as Jellyfin's bitrate test
// does, but of the file itself. It is no playback: nothing is recorded of it.
func (a *API) partSample(w http.ResponseWriter, r *http.Request) {
	visible := func(ctx context.Context, part uuid.UUID) (string, string, error) {
		return a.svc.Playing.VisiblePartFile(ctx, auth.SessionOf(ctx).Profile.ID, part)
	}
	a.serveLibraryFile(w, r, "id", visible, sampleBytes)
}

// subtitleFormat is how a subtitle file beside a copy is served.
type subtitleFormat string

const (
	subtitleOriginal subtitleFormat = "original"
	subtitleWebVTT   subtitleFormat = "webvtt"
)

func subtitleFormats() []subtitleFormat { return []subtitleFormat{subtitleOriginal, subtitleWebVTT} }

// subtitleFile serves a subtitle file beside a copy as it is, or a text one converted to WebVTT,
// as Jellyfin's subtitle route converts, for a player that draws nothing else.
func (a *API) subtitleFile(w http.ResponseWriter, r *http.Request) {
	format, ok := queryEnum(a, w, r, "format", subtitleOriginal, subtitleFormats())
	if !ok {
		return
	}
	switch format {
	case subtitleOriginal:
		a.serveLibraryFile(w, r, "id", a.svc.Playing.SubtitleFile, math.MaxInt64)
	case subtitleWebVTT:
		a.subtitleVTT(w, r)
	}
}

func (a *API) subtitleVTT(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r, "id")
	if !ok {
		return
	}
	sub, err := a.svc.Playing.Subtitle(r.Context(), id)
	if a.answered(w, r, err) {
		return
	}
	if !hls.TextSubtitle(sub.Codec) {
		writeProblem(w, a.logger, codeInvalidParameter, "format webvtt is for plain text subtitles: WebVTT carries no pictures, and would lose a styled one's look")
		return
	}
	ctx := r.Context()
	open := func() (*os.File, error) {
		f, _, err := openLibraryFile(ctx, a.svc.Playing.SubtitleFile, id)
		return f, err
	}
	vtt, err := a.svc.HLS.WebVTT(ctx, open, domain.TagOf(sub.Language))
	if a.answered(w, r, err) {
		return
	}
	w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
	writeBody(w, a.logger, []byte(vtt))
}

// serveLibraryFile serves the first limit bytes of the file of a library that where finds for the
// id the path names by param: only a file the scanner recorded.
func (a *API) serveLibraryFile(w http.ResponseWriter, r *http.Request, param string, where func(context.Context, uuid.UUID) (string, string, error), limit int64) {
	id, ok := a.pathID(w, r, param)
	if !ok {
		return
	}
	f, rel, err := openLibraryFile(r.Context(), where, id)
	if a.answered(w, r, err) {
		return
	}
	defer f.Close()
	if err := library.Serve(w, r, f, rel, limit); err != nil {
		a.internal(w, r, err)
	}
}

// openLibraryFile opens the file of a library that where finds for an id.
func openLibraryFile(ctx context.Context, where func(context.Context, uuid.UUID) (string, string, error), id uuid.UUID) (*os.File, string, error) {
	root, rel, err := where(ctx, id)
	if err != nil {
		return nil, "", err
	}
	f, err := library.Open(root, rel)
	return f, rel, err
}

// requireSignature admits a request whose address the server signed and which has not lapsed.
func (a *API) requireSignature(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if !a.svc.Signer.Valid(r.URL.Path, q.Get("exp"), q.Get("sig"), time.Now()) {
			writeProblem(w, a.logger, codeUnauthenticated, "the address is not signed, or has lapsed")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *API) streamsRoutes() []route {
	return []route{
		{
			pattern: "GET /api/v1/playbacks/{id}/parts/{part}/stream", access: signedAddress,
			summary: "A copy's file as it is, in byte ranges, at the address play answered, for as long as the playback lasts",
			query:   signatureParams, status: http.StatusOK, reply: asFile{"video/*"}, delivery: playback.DeliveryFile,
			handle: a.playbackPartStream,
		},
		{
			pattern: "GET /api/v1/parts/{id}/stream", access: signedAddress,
			summary: "A copy's file as it is, in byte ranges, at the address a download answered",
			query:   signatureParams, status: http.StatusOK, reply: asFile{"video/*"}, delivery: playback.DeliveryFile,
			handle: a.partStream,
		},
		{
			pattern: "GET /api/v1/parts/{id}/sample", access: signedIn,
			summary: "The first " + strconv.Itoa(sampleBytes>>20) + " MiB of a part's file, in byte ranges, to time the connection; no playback",
			status:  http.StatusOK, reply: asFile{"video/*"}, handle: a.partSample,
		},
		{
			pattern: "GET /api/v1/subtitles/{id}/file", access: signedAddress,
			summary: "A subtitle file beside a copy, as it is or as WebVTT, at the address play answered",
			query: append([]param{
				{"format", subtitleOriginal, "webvtt converts a text subtitle to WebVTT; original, the default, is the file as it is."},
			}, signatureParams...),
			status: http.StatusOK, reply: asFile{"application/x-subrip", "text/vtt", "text/x-ssa"}, handle: a.subtitleFile,
		},
	}
}
