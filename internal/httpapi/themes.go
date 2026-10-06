package httpapi

import (
	"context"
	"net/http"
	"os"
	"path"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/library"
	"github.com/olivertgwalton/photon-server/internal/naming"
	"github.com/olivertgwalton/photon-server/internal/store"
)

type themes interface {
	Theme(ctx context.Context, id uuid.UUID) (store.ThemeFile, error)
}

// theme serves a theme tune as a picture is served: public, so a player that sends no headers of
// its own plays it, and kept for good, as its id changes whenever the file does.
func (a *API) theme(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeProblem(w, a.logger, codeNotFound, "")
		return
	}
	t, err := a.svc.Themes.Theme(r.Context(), id)
	if a.answered(w, r, err) {
		return
	}
	var f *os.File
	if t.URL != "" {
		f, err = a.svc.Artwork.Sound(r.Context(), id, t.URL)
	} else {
		f, err = library.Open(t.Root, t.Path)
	}
	if a.answered(w, r, err) {
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		a.internal(w, r, err)
		return
	}
	h := w.Header()
	// The standard library knows no sound file's type by its name.
	if kind, ok := naming.AudioType(path.Base(t.Path + t.URL)); ok {
		h.Set("Content-Type", kind)
	}
	h.Set("Cache-Control", "public, max-age=31536000, immutable")
	h.Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, "", info.ModTime(), f)
}
