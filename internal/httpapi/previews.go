package httpapi

import (
	"context"
	"maps"
	"net/http"
	"os"
	"strconv"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/store"
)

type previews interface {
	Trickplay(ctx context.Context, profile, part uuid.UUID) (store.Trickplay, error)
	HasChapterImage(ctx context.Context, profile, part uuid.UUID, idx int) error
}

type previewFiles interface {
	Sheet(part uuid.UUID, n int) (*os.File, error)
	ChapterImage(part uuid.UUID, idx int) (*os.File, error)
}

// trickplay answers how a part's thumbnail sheets are laid out, so a client can find the
// thumbnail for any time: sheet floor(t / interval / (columns × rows)), counted from zero.
func (a *API) trickplay(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r)
	if !ok {
		return
	}
	t, err := a.svc.Previews.Trickplay(r.Context(), sessionOf(r).Profile.ID, id)
	if a.answered(w, r, err) {
		return
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, t)
}

func (a *API) trickplaySheet(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r)
	if !ok {
		return
	}
	n, err := strconv.Atoi(r.PathValue("n"))
	if err != nil {
		writeProblem(w, a.logger, codeNotFound, "")
		return
	}
	t, err := a.svc.Previews.Trickplay(r.Context(), sessionOf(r).Profile.ID, id)
	if a.answered(w, r, err) {
		return
	}
	if n < 0 || n >= t.Sheets {
		writeProblem(w, a.logger, codeNotFound, "")
		return
	}
	a.servePreview(w, r, func() (*os.File, error) { return a.svc.PreviewFiles.Sheet(id, n) })
}

func (a *API) chapterImage(w http.ResponseWriter, r *http.Request) {
	part, idx, ok := a.chapterOf(w, r)
	if !ok || a.answered(w, r, a.svc.Previews.HasChapterImage(r.Context(), sessionOf(r).Profile.ID, part, idx)) {
		return
	}
	a.servePreview(w, r, func() (*os.File, error) { return a.svc.PreviewFiles.ChapterImage(part, idx) })
}

// signedChapterImage serves a chapter's picture at the address a title's page signed for the
// profile that could see it.
func (a *API) signedChapterImage(w http.ResponseWriter, r *http.Request) {
	part, idx, ok := a.chapterOf(w, r)
	if !ok {
		return
	}
	a.servePreview(w, r, func() (*os.File, error) { return a.svc.PreviewFiles.ChapterImage(part, idx) })
}

// chapterOf reads the part and chapter a path names.
func (a *API) chapterOf(w http.ResponseWriter, r *http.Request) (uuid.UUID, int, bool) {
	id, ok := a.pathID(w, r)
	if !ok {
		return id, 0, false
	}
	idx, err := strconv.Atoi(r.PathValue("idx"))
	if err != nil {
		writeProblem(w, a.logger, codeNotFound, "")
		return id, 0, false
	}
	return id, idx, true
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
