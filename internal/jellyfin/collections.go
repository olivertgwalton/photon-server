package jellyfin

import (
	"net/http"

	"github.com/olivertgwalton/photon-server/internal/auth"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// collectionsView is the name of the view of every collection the profile sees, which Jellyfin's
// apps find collections by.
const collectionsView = "Collections"

func (a *API) collectionsFolder() item {
	return a.view(collectionsView, "CollectionFolder", "boxsets")
}

// collections answers the collections holding one library's titles where an app names that
// library, or every collection, in no library, as Jellyfin's box sets are; by title either way.
func (a *API) collections(libs []*store.SeenLibrary, w http.ResponseWriter, r *http.Request, l listed) {
	profile := auth.SessionOf(r.Context()).Profile.ID
	var cards []store.Card
	var total int64
	var err error
	if len(libs) == 1 {
		cards, total, err = a.svc.Catalogue.Collections(r.Context(), libs[0].ID, profile, l.start, l.limit)
	} else {
		cards, total, err = a.svc.Catalogue.AllCollections(r.Context(), profile, l.start, l.limit)
	}
	if err != nil {
		a.internal(w, r, err)
		return
	}
	a.writeList(w, r, cards, int(total), l.start, l)
}
