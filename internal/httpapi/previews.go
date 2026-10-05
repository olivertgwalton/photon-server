package httpapi

import (
	"context"
	"errors"
	"io/fs"
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
	id, ok := a.pathID(w, r)
	if !ok {
		return
	}
	idx, err := strconv.Atoi(r.PathValue("idx"))
	if err != nil {
		writeProblem(w, a.logger, codeNotFound, "")
		return
	}
	if a.answered(w, r, a.svc.Previews.HasChapterImage(r.Context(), sessionOf(r).Profile.ID, id, idx)) {
		return
	}
	a.servePreview(w, r, func() (*os.File, error) { return a.svc.PreviewFiles.ChapterImage(id, idx) })
}

// servePreview serves a preview's JPEG. A part's previews are made again only from the same bytes,
// so a client may keep one as long as it likes.
func (a *API) servePreview(w http.ResponseWriter, r *http.Request, open func() (*os.File, error)) {
	f, err := open()
	if errors.Is(err, fs.ErrNotExist) {
		writeProblem(w, a.logger, codeNotFound, "")
		return
	}
	if err != nil {
		a.internal(w, r, err)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		a.internal(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	http.ServeContent(w, r, "", info.ModTime(), f)
}
