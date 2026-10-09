package httpapi

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/hls"
	"github.com/olivertgwalton/photon-server/internal/media"
)

// fontTypes are the types of the fonts a file carries for its styled subtitles.
var fontTypes = map[string]string{
	".ttf": "font/ttf", ".otf": "font/otf", ".ttc": "font/collection", ".woff": "font/woff", ".woff2": "font/woff2",
}

type fontsJSON struct {
	Fonts []fontJSON `json:"fonts"`
}

// fontJSON is a font a file carries, at an address signed as long as the list's own.
type fontJSON struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// partSubtitles is a part's file, opened as the scanner recorded it, and its streams, for reading
// its subtitles out. The read outlives the request that starts it.
func (a *API) partSubtitles(ctx context.Context, part uuid.UUID) (hls.SubtitleSource, error) {
	streams, err := a.svc.Playing.PartStreams(ctx, part)
	if err != nil {
		return hls.SubtitleSource{}, err
	}
	opening := context.WithoutCancel(ctx)
	open := func() (media.Input, error) { return a.svc.Parts.Open(opening, part) }
	return hls.SubtitleSource{Open: open, Part: part, Streams: streams}, nil
}

// styledStream serves a styled subtitle stream of a part, read out as it is.
func (a *API) styledStream(w http.ResponseWriter, r *http.Request) {
	part, ok := a.pathID(w, r, "id")
	if !ok {
		return
	}
	n, err := strconv.Atoi(r.PathValue("subtitle"))
	if err != nil {
		writeProblem(w, a.logger, codeNotFound, "")
		return
	}
	src, err := a.partSubtitles(r.Context(), part)
	if a.answered(w, r, err) {
		return
	}
	if !slices.ContainsFunc(src.Streams, func(s domain.Stream) bool {
		return s.Index == n && s.Kind == domain.StreamSubtitle && hls.StyledSubtitle(s.Codec)
	}) {
		writeProblem(w, a.logger, codeNotFound, "")
		return
	}
	dir, err := a.svc.HLS.Extracted(r.Context(), src, hls.StyledName(n))
	if a.answered(w, r, err) {
		return
	}
	f, err := os.Open(filepath.Join(dir, hls.StyledName(n)))
	if err != nil {
		a.internal(w, r, err)
		return
	}
	a.serveFile(w, r, f, hls.StyledName(n), http.Header{"Content-Type": {"text/x-ssa; charset=utf-8"}})
}

// partFonts lists the fonts a part's file carries for its styled subtitles, each at an address
// signed until the list's own lapses.
func (a *API) partFonts(w http.ResponseWriter, r *http.Request) {
	part, ok := a.pathID(w, r, "id")
	if !ok {
		return
	}
	src, err := a.partSubtitles(r.Context(), part)
	if a.answered(w, r, err) {
		return
	}
	dir, err := a.svc.HLS.Extracted(r.Context(), src, hls.FontsDir)
	if a.answered(w, r, err) {
		return
	}
	entries, err := os.ReadDir(filepath.Join(dir, hls.FontsDir))
	if err != nil {
		a.internal(w, r, err)
		return
	}
	// requireSignature has read exp already.
	exp, err := strconv.ParseInt(r.URL.Query().Get("exp"), 10, 64)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	answer := fontsJSON{Fonts: []fontJSON{}}
	for _, e := range entries {
		if _, ok := fontTypes[strings.ToLower(filepath.Ext(e.Name()))]; !ok || !e.Type().IsRegular() {
			continue
		}
		at := r.URL.Path + "/" + e.Name()
		expires, sig := a.svc.Signer.Token(at, time.Unix(exp, 0))
		u := url.URL{Path: at, RawQuery: url.Values{"exp": {expires}, "sig": {sig}}.Encode()}
		answer.Fonts = append(answer.Fonts, fontJSON{Name: e.Name(), URL: u.String()})
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, answer)
}

// partFont serves a font a part's file carries, as partFonts listed it.
func (a *API) partFont(w http.ResponseWriter, r *http.Request) {
	part, ok := a.pathID(w, r, "id")
	if !ok {
		return
	}
	name := r.PathValue("name")
	t, ok := fontTypes[strings.ToLower(filepath.Ext(name))]
	if !ok {
		writeProblem(w, a.logger, codeNotFound, "")
		return
	}
	src, err := a.partSubtitles(r.Context(), part)
	if a.answered(w, r, err) {
		return
	}
	dir, err := a.svc.HLS.Extracted(r.Context(), src, hls.FontsDir)
	if a.answered(w, r, err) {
		return
	}
	f, err := os.OpenInRoot(filepath.Join(dir, hls.FontsDir), name)
	if errors.Is(err, fs.ErrNotExist) {
		writeProblem(w, a.logger, codeNotFound, "")
		return
	}
	if err != nil {
		a.internal(w, r, err)
		return
	}
	// A font's bytes never change under its address: the part's file is read out once.
	a.serveFile(w, r, f, name, http.Header{"Content-Type": {t}, "Cache-Control": {"private, max-age=86400"}})
}

func (a *API) styledRoutes() []route {
	return []route{
		{
			pattern: "GET /api/v1/parts/{id}/subtitles/{subtitle}", access: signedAddress,
			summary: "A styled subtitle stream of a part, read out as it is, at the address play answered",
			path:    []param{{"subtitle", "", "The stream's index in the part's file."}},
			query:   signatureParams, status: http.StatusOK, reply: asFile{"text/x-ssa"}, handle: a.styledStream,
		},
		{
			pattern: "GET /api/v1/parts/{id}/fonts", access: signedAddress,
			summary: "List the fonts a part's file carries for its styled subtitles, at the address play answered",
			query:   signatureParams, status: http.StatusOK, reply: fontsJSON{}, handle: a.partFonts,
		},
		{
			pattern: "GET /api/v1/parts/{id}/fonts/{name}", access: signedAddress,
			summary: "A font a part's file carries, at the address its list answered",
			path:    []param{{"name", "", "The font's name, as its list answered it."}},
			query:   signatureParams, status: http.StatusOK, reply: asFile{"font/ttf", "font/otf", "font/collection", "font/woff", "font/woff2"},
			handle: a.partFont,
		},
	}
}
