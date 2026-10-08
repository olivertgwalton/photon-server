package jellyfin

import (
	"net/http"

	"github.com/olivertgwalton/photon-server/internal/auth"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// collections answers the collections of libraries, each library's by title, library after library
// in the profile's order.
func (a *API) collections(libs []*store.SeenLibrary, w http.ResponseWriter, r *http.Request, l listed) {
	var cards []store.Card
	total, skip := 0, l.start
	for _, lib := range libs {
		got, n, err := a.svc.Catalogue.Collections(r.Context(), lib.ID, auth.SessionOf(r.Context()).Profile.ID, skip, l.limit-len(cards))
		if err != nil {
			a.internal(w, r, err)
			return
		}
		cards, total, skip = append(cards, got...), total+int(n), max(skip-int(n), 0)
	}
	a.writeList(w, r, cards, total, l.start, l)
}
