package httpapi

import (
	"context"
	"io"
	"math"
	"net/http"
	"os"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

type pictures interface {
	Picture(ctx context.Context, id uuid.UUID) (domain.Picture, error)
}

type pictureCache interface {
	Open(ctx context.Context, id uuid.UUID, p domain.Picture, width, height int) (*os.File, string, error)
	Keep(id uuid.UUID, r io.Reader) error
	Kept(id uuid.UUID) (*os.File, error)
}

// artwork serves a picture, or with width or height a copy that fits inside them, for a client that
// does not size pictures itself. Its id changes whenever the picture does, so a client keeps it for good. It is
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
	width, ok := a.queryNumber(w, r, "width", 0, 1, math.MaxInt)
	if !ok {
		return
	}
	height, ok := a.queryNumber(w, r, "height", 0, 1, math.MaxInt)
	if !ok {
		return
	}
	f, name, err := a.svc.Artwork.Open(r.Context(), id, pic, width, height)
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
