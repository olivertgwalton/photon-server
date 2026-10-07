package httpapi

import (
	"context"
	"net/http"
	"os"
	"path"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/blob"
	"github.com/olivertgwalton/photon-server/internal/domain"
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
	id, ok := a.pathID(w, r, "id")
	if !ok {
		return
	}
	t, err := a.svc.Themes.Theme(r.Context(), id)
	if a.answered(w, r, err) {
		return
	}
	var o blob.Object
	// The standard library knows no sound file's type by its name.
	var kind string
	switch t.Source {
	case domain.ThemeFromFile:
		var f *os.File
		if f, err = library.Open(t.Root, t.Path); err == nil {
			o, err = blob.OfFile(f)
		}
		kind, _ = naming.AudioType(path.Base(t.Path))
	case domain.ThemeFromThemerr:
		o, err = a.svc.Artwork.Kept(r.Context(), id)
		kind = "audio/mp4"
	}
	if a.answered(w, r, err) {
		return
	}
	h := http.Header{
		"Cache-Control":          {"public, max-age=31536000, immutable"},
		"X-Content-Type-Options": {"nosniff"},
	}
	if kind != "" {
		h.Set("Content-Type", kind)
	}
	a.serveObject(w, r, o, "", h)
}
