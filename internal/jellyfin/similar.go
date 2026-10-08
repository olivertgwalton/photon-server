package jellyfin

import (
	"net/http"

	"github.com/olivertgwalton/photon-server/internal/auth"
)

// similar answers the films or shows most like a title, as photon's own "More like this" ranks
// them, as many of them as an app asks for.
func (a *API) similar(w http.ResponseWriter, r *http.Request) {
	id, ok := a.itemID(w, r)
	if !ok {
		return
	}
	cards, err := a.svc.Catalogue.Similar(r.Context(), auth.SessionOf(r.Context()).Profile.ID, id)
	if isNotFound(err) {
		a.refuse(w, http.StatusNotFound)
		return
	}
	if err != nil {
		a.internal(w, r, err)
		return
	}
	l := listedOf(w, r)
	a.writeList(w, r, cards[:min(l.limit, len(cards))], len(cards), 0, l)
}
