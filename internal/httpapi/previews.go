package httpapi

import (
	"context"
	"net/http"
	"os"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/blob"
	"github.com/olivertgwalton/photon-server/internal/store"
)

type previews interface {
	Trickplay(ctx context.Context, profile, part uuid.UUID) (store.Trickplay, error)
}

type previewFiles interface {
	Sheet(ctx context.Context, part uuid.UUID, n int) (blob.Object, error)
	ChapterImage(ctx context.Context, part uuid.UUID, idx int) (blob.Object, error)
}

type trickplayJSON struct {
	Width      int `json:"width"`
	Height     int `json:"height"`
	IntervalMS int `json:"interval_ms"`
	Columns    int `json:"columns"`
	Rows       int `json:"rows"`
	Thumbnails int `json:"thumbnails"`
	Sheets     int `json:"sheets"`
}

// trickplay answers how a part's thumbnail sheets are laid out, so a client can find the
// thumbnail for any time: sheet floor(t / interval / (columns × rows)), counted from zero.
func (a *API) trickplay(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r, "id")
	if !ok {
		return
	}
	t, err := a.svc.Previews.Trickplay(r.Context(), sessionOf(r).Profile.ID, id)
	if a.answered(w, r, err) {
		return
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, trickplayJSON(t))
}

func (a *API) trickplaySheet(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r, "id")
	if !ok {
		return
	}
	n, ok := a.pathNumber(w, r, "n")
	if !ok {
		return
	}
	t, err := a.svc.Previews.Trickplay(r.Context(), sessionOf(r).Profile.ID, id)
	if a.answered(w, r, err) {
		return
	}
	if n >= t.Sheets {
		writeProblem(w, a.logger, codeNotFound, "")
		return
	}
	a.servePreview(w, r, func(ctx context.Context) (blob.Object, error) { return a.svc.PreviewFiles.Sheet(ctx, id, n) })
}

// chapterImage serves a chapter's picture at the address a title's page signed for the profile
// that could see it.
func (a *API) chapterImage(w http.ResponseWriter, r *http.Request) {
	part, ok := a.pathID(w, r, "id")
	if !ok {
		return
	}
	idx, ok := a.pathNumber(w, r, "idx")
	if !ok {
		return
	}
	a.servePreview(w, r, func(ctx context.Context) (blob.Object, error) { return a.svc.PreviewFiles.ChapterImage(ctx, part, idx) })
}

// servePreview serves a preview's JPEG. A part's previews are made again only from the same bytes,
// so a client may keep one as long as it likes.
func (a *API) servePreview(w http.ResponseWriter, r *http.Request, open func(context.Context) (blob.Object, error)) {
	o, err := open(r.Context())
	if a.answered(w, r, err) {
		return
	}
	a.serveObject(w, r, o, "", http.Header{
		"Content-Type":  {"image/jpeg"},
		"Cache-Control": {"private, max-age=31536000, immutable"},
	})
}

// serveFile serves f, which it closes, in byte ranges. It sets header only once f has answered, so
// a failure carries none of it; name gives the type where header does not.
func (a *API) serveFile(w http.ResponseWriter, r *http.Request, f *os.File, name string, header http.Header) {
	o, err := blob.OfFile(f)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	a.serveObject(w, r, o, name, header)
}

// serveObject is serveFile for an object.
func (a *API) serveObject(w http.ResponseWriter, r *http.Request, o blob.Object, name string, header http.Header) {
	if err := blob.Serve(w, r, o, name, header); err != nil {
		a.internal(w, r, err)
	}
}

func (a *API) previewsRoutes() []route {
	return []route{
		{
			pattern: "GET /api/v1/parts/{id}/trickplay", access: signedIn,
			summary: "How a part's trickplay sheets are laid out, to find the thumbnail for a time",
			status:  http.StatusOK, reply: trickplayJSON{}, handle: a.trickplay,
		},
		{
			pattern: "GET /api/v1/parts/{id}/trickplay/{n}", access: signedIn, summary: "A part's trickplay sheet",
			path:   []param{{"n", 0, "The sheet, counted from 0."}},
			status: http.StatusOK, reply: asFile{"image/jpeg"}, handle: a.trickplaySheet,
		},
		{
			pattern: "GET /api/v1/parts/{id}/chapters/{idx}/image", access: signedAddress,
			summary: "A picture of a chapter, at the signed address the title's page gives",
			path:    []param{{"idx", 0, "The chapter, counted from 0 in its part."}},
			query:   signatureParams, status: http.StatusOK, reply: asFile{"image/jpeg"}, handle: a.chapterImage,
		},
	}
}
