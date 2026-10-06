package httpapi

import (
	"context"
	"maps"
	"net/http"
	"os"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/store"
)

type previews interface {
	Trickplay(ctx context.Context, profile, part uuid.UUID) (store.Trickplay, error)
}

type previewFiles interface {
	Sheet(part uuid.UUID, n int) (*os.File, error)
	ChapterImage(part uuid.UUID, idx int) (*os.File, error)
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
	a.servePreview(w, r, func() (*os.File, error) { return a.svc.PreviewFiles.Sheet(id, n) })
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
	a.servePreview(w, r, func() (*os.File, error) { return a.svc.PreviewFiles.ChapterImage(part, idx) })
}

// servePreview serves a preview's JPEG. A part's previews are made again only from the same bytes,
// so a client may keep one as long as it likes.
func (a *API) servePreview(w http.ResponseWriter, r *http.Request, open func() (*os.File, error)) {
	f, err := open()
	if a.answered(w, r, err) {
		return
	}
	a.serveFile(w, r, f, "", http.Header{
		"Content-Type":  {"image/jpeg"},
		"Cache-Control": {"private, max-age=31536000, immutable"},
	})
}

// serveFile serves f, which it closes, in byte ranges. It sets header only once f has answered, so
// a failure carries none of it; name gives the type where header does not.
func (a *API) serveFile(w http.ResponseWriter, r *http.Request, f *os.File, name string, header http.Header) {
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		a.internal(w, r, err)
		return
	}
	maps.Copy(w.Header(), header)
	http.ServeContent(w, r, name, info.ModTime(), f)
}
