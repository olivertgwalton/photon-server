package httpapi

import (
	"context"
	"errors"
	"io"
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
	Resized(ctx context.Context, key string, width int, open func(context.Context) (*os.File, error)) (*os.File, error)
	Keep(id uuid.UUID, r io.Reader) error
	Kept(id uuid.UUID) (*os.File, error)
}

// artwork serves a picture, or with width a copy that wide, for a client that does not size
// pictures itself. Its id changes whenever the picture does, so a client keeps it for good. It is
// public, as Jellyfin's are, so a page can show it without a token; ids are random.
func (a *API) artwork(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r, "id")
	if !ok {
		return
	}
	pic, err := a.svc.Pictures.Picture(r.Context(), id)
	if a.answered(w, r, err) {
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
	if a.answered(w, r, err) {
		return
	}
	a.serveFile(w, r, f, name, http.Header{
		"Cache-Control": {"public, max-age=31536000, immutable"},
		// A provider's logo may be SVG, which a browser opening it directly would run script in.
		"Content-Security-Policy": {"default-src 'none'; style-src 'unsafe-inline'; sandbox"},
		"X-Content-Type-Options":  {"nosniff"},
	})
}

func (a *API) openPicture(ctx context.Context, id uuid.UUID, pic store.Picture, width int) (*os.File, string, error) {
	name := path.Base(pic.Path + pic.URL)
	original := func(ctx context.Context) (*os.File, error) {
		switch {
		case pic.Kept:
			return a.svc.Artwork.Kept(id)
		case pic.URL != "":
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
	f, err := original(ctx)
	return f, name, err
}
