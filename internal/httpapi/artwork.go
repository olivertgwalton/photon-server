package httpapi

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path"
	"strconv"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/artwork"
	"github.com/olivertgwalton/photon-server/internal/library"
	"github.com/olivertgwalton/photon-server/internal/store"
)

type pictures interface {
	Picture(ctx context.Context, id uuid.UUID) (store.Picture, error)
}

type pictureCache interface {
	File(ctx context.Context, id uuid.UUID, url string) (*os.File, error)
	Resized(ctx context.Context, key string, width int, open func() (*os.File, error)) (*os.File, error)
}

// artwork serves a picture, or with width a copy that wide, for a client that does not size
// pictures itself. Its id changes whenever the picture does, so a client keeps it for good. It is
// public, as Jellyfin's are, so a page can show it without a token; ids are random.
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
	var width int
	if v := r.URL.Query().Get("width"); v != "" {
		if width, err = strconv.Atoi(v); err != nil || width < 1 {
			writeProblem(w, a.logger, codeInvalidParameter, "width is a number of pixels")
			return
		}
	}
	f, name, err := a.openPicture(r.Context(), id, pic, width)
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

func (a *API) openPicture(ctx context.Context, id uuid.UUID, pic store.Picture, width int) (*os.File, string, error) {
	name := path.Base(pic.Path + pic.URL)
	original := func() (*os.File, error) {
		if pic.URL != "" {
			return a.svc.Artwork.File(ctx, id, pic.URL)
		}
		return library.Open(pic.Root, pic.Path)
	}
	if width > 0 {
		f, err := a.svc.Artwork.Resized(ctx, id.String(), width, original)
		// A resized copy is named for no format; its content says which.
		if !errors.Is(err, artwork.ErrNotResizable) {
			return f, "", err
		}
	}
	f, err := original()
	return f, name, err
}
