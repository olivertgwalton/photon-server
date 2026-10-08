package jellyfin

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/blob"
	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// pictureFiles are pictures, and the theme tunes fetched for titles, as artwork's cache keeps them.
type pictureFiles interface {
	Open(ctx context.Context, id uuid.UUID, p domain.Picture, width, height int) (blob.Object, string, error)
	Kept(ctx context.Context, id uuid.UUID) (blob.Object, error)
}

// image answers a picture by its tag, sized to fit what an app asks for. A tag is a picture's id,
// which changes whenever the picture does, so it is kept for good. It is public, as Jellyfin's
// images are, so a page can show one without a token; ids are random.
func (a *API) image(w http.ResponseWriter, r *http.Request) {
	if strings.EqualFold(r.PathValue("imageType"), "Chapter") {
		a.chapterImage(w, r)
		return
	}
	id, err := uuid.Parse(query(r, "tag"))
	if err != nil {
		a.refuse(w, http.StatusNotFound)
		return
	}
	pic, err := a.svc.Catalogue.Picture(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		a.refuse(w, http.StatusNotFound)
		return
	}
	if err != nil {
		a.internal(w, r, err)
		return
	}
	bound := func(names ...string) int {
		n := 0
		for _, name := range names {
			if v, err := strconv.Atoi(query(r, name)); err == nil && v > 0 {
				n = max(n, v)
			}
		}
		return n
	}
	o, name, err := a.svc.Pictures.Open(r.Context(), id, pic, bound("maxWidth", "fillWidth", "width"), bound("maxHeight", "fillHeight", "height"))
	if errors.Is(err, os.ErrNotExist) {
		a.refuse(w, http.StatusNotFound)
		return
	}
	if err != nil {
		a.internal(w, r, err)
		return
	}
	err = blob.Serve(w, r, o, name, http.Header{
		"Cache-Control": {"public, max-age=31536000, immutable"},
		// A provider's logo may be SVG, which a browser opening it directly would run script in.
		"Content-Security-Policy": {"default-src 'none'; style-src 'unsafe-inline'; sandbox"},
		"X-Content-Type-Options":  {"nosniff"},
	})
	if err != nil {
		a.internal(w, r, err)
	}
}
