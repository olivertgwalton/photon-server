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

// pictureKinds are the pictures photon has for each kind an app asks an item for, the first of
// them it has: a title's Primary is its poster, an episode's its still.
var pictureKinds = map[string][]domain.ArtworkKind{
	"primary":  {domain.ArtworkPoster, domain.ArtworkThumb},
	"backdrop": {domain.ArtworkBackdrop},
	"logo":     {domain.ArtworkLogo},
	"thumb":    {domain.ArtworkThumb},
	"banner":   {domain.ArtworkBanner},
}

// image answers a picture by its tag, sized to fit what an app asks for. A tag is a picture's id,
// which changes whenever the picture does, so it is kept for good. An app may name no tag, as
// Jellyfin's tag is optional, and is answered the item's best picture of the kind it asks for:
// an app draws an episode by its season's poster, which it can ask for no other way, for nothing
// on an episode names it. It is public, as Jellyfin's images are, so a page can show one without
// a token; ids are random.
func (a *API) image(w http.ResponseWriter, r *http.Request) {
	if strings.EqualFold(r.PathValue("imageType"), "Chapter") {
		a.chapterImage(w, r)
		return
	}
	id, err := uuid.Parse(query(r, "tag"))
	tagged := err == nil
	if !tagged {
		id, err = a.itemPicture(r)
		if errors.Is(err, store.ErrNotFound) {
			a.refuse(w, http.StatusNotFound)
			return
		}
		if err != nil {
			a.internal(w, r, err)
			return
		}
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
	// A picture named by its tag is kept for good; the best of an item's changes as its pictures
	// do, so one answered by the item's id is kept for an hour.
	cache := "public, max-age=31536000, immutable"
	if !tagged {
		cache = "public, max-age=3600"
	}
	err = blob.Serve(w, r, o, name, http.Header{
		"Cache-Control": {cache},
		// A provider's logo may be SVG, which a browser opening it directly would run script in.
		"Content-Security-Policy": {"default-src 'none'; style-src 'unsafe-inline'; sandbox"},
		"X-Content-Type-Options":  {"nosniff"},
	})
	if err != nil {
		a.internal(w, r, err)
	}
}

// itemPicture is the best picture of the item an app names in the path, of the kind it asks for.
// ErrNotFound where it names neither.
func (a *API) itemPicture(r *http.Request) (uuid.UUID, error) {
	item, err := uuid.Parse(r.PathValue("itemId"))
	kinds, ok := pictureKinds[strings.ToLower(r.PathValue("imageType"))]
	if err != nil || !ok {
		return uuid.UUID{}, store.ErrNotFound
	}
	return a.svc.Catalogue.BestPicture(r.Context(), item, kinds)
}
