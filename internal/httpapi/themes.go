package httpapi

import (
	"context"
	"net/http"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/artwork"
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
	o, kind, err := artwork.OpenTheme(r.Context(), a.svc.Artwork, id, t.Source, t.Root, t.Path)
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

func (a *API) themesRoutes() []route {
	return []route{
		{
			pattern: "GET /api/v1/themes/{id}", access: public,
			summary: "A theme tune, in byte ranges, kept for good: its id changes when it does",
			status:  http.StatusOK, reply: asFile{"audio/*"}, handle: a.theme,
		},
	}
}
