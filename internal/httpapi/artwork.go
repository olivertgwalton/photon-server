package httpapi

import (
	"context"
	"io"
	"math"
	"net/http"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/blob"
	"github.com/olivertgwalton/photon-server/internal/domain"
)

type pictures interface {
	Picture(ctx context.Context, id uuid.UUID) (domain.Picture, error)
}

type pictureCache interface {
	Open(ctx context.Context, id uuid.UUID, p domain.Picture, width, height int) (blob.Object, string, error)
	Keep(ctx context.Context, id uuid.UUID, r io.Reader) error
	Kept(ctx context.Context, id uuid.UUID) (blob.Object, error)
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
	o, name, err := a.svc.Artwork.Open(r.Context(), id, pic, width, height)
	if a.answered(w, r, err) {
		return
	}
	a.serveObject(w, r, o, name, http.Header{
		"Cache-Control": {"public, max-age=31536000, immutable"},
		// A provider's logo may be SVG, which a browser opening it directly would run script in.
		"Content-Security-Policy": {"default-src 'none'; style-src 'unsafe-inline'; sandbox"},
		"X-Content-Type-Options":  {"nosniff"},
	})
}

func (a *API) artworkRoutes() []route {
	return []route{
		{
			pattern: "GET /api/v1/artwork/{id}", access: public,
			summary: "A picture, kept for good: its id changes when it does",
			query: []param{
				{"width", 0, "A copy at most this many pixels wide, keeping its shape; rounded up to one of a few sizes."},
				{"height", 0, "A copy at most this many pixels high, keeping its shape; rounded up as width is."},
			},
			status: http.StatusOK, reply: asFile{"image/*"}, handle: a.artwork,
		},
	}
}
