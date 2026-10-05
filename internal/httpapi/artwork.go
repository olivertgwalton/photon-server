package httpapi

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/store"
)

type pictures interface {
	Picture(ctx context.Context, id uuid.UUID) (store.Picture, error)
}

type pictureCache interface {
	File(ctx context.Context, id uuid.UUID, url string) (*os.File, error)
}

// artwork serves a picture. Its id changes whenever the picture does, so a client keeps it for
// good. It is public, as Jellyfin's are, so a page can show it without a token; ids are random.
func (a *API) artwork(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeProblem(w, a.logger, codeNotFound, "")
		return
	}
	pic, err := a.svc.Pictures.Picture(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeProblem(w, a.logger, codeNotFound, "")
		return
	}
	if err != nil {
		a.internal(w, r, err)
		return
	}
	f, name, err := a.openPicture(r.Context(), id, pic)
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
	h := w.Header()
	h.Set("Cache-Control", "public, max-age=31536000, immutable")
	// A provider's logo may be SVG, which a browser opening it directly would run script in.
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	h.Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, name, info.ModTime(), f)
}

func (a *API) openPicture(ctx context.Context, id uuid.UUID, pic store.Picture) (*os.File, string, error) {
	if pic.URL != "" {
		f, err := a.svc.Artwork.File(ctx, id, pic.URL)
		return f, path.Base(pic.URL), err
	}
	root, err := os.OpenRoot(pic.Root)
	if err != nil {
		return nil, "", err
	}
	defer root.Close()
	f, err := root.Open(pic.Path)
	return f, path.Base(pic.Path), err
}
