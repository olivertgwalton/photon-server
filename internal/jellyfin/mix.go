package jellyfin

import (
	"net/http"

	"github.com/olivertgwalton/photon-server/internal/auth"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// instantMix answers Jellyfin's instant mix of a track, an album or an artist: tracks of its
// genres, the track first. A genre's, which names no item, is genreMix.
func (a *API) instantMix(w http.ResponseWriter, r *http.Request) {
	if r.PathValue("itemId") == "" {
		a.genreMix(w, r)
		return
	}
	id, ok := a.itemID(w, r)
	if !ok {
		return
	}
	cards, err := a.svc.Catalogue.Mix(r.Context(), auth.SessionOf(r.Context()).Profile.ID, id)
	if isNotFound(err) {
		a.refuse(w, http.StatusNotFound)
		return
	}
	a.writeMix(w, r, cards, err)
}

// genreMix answers Jellyfin's instant mix of a genre, which an app names by the id /Genres gives it.
func (a *API) genreMix(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(query(r, "id"))
	if !ok {
		a.refuse(w, http.StatusNotFound)
		return
	}
	libs, _, err := a.seenLibraries(r)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	name, ok, err := a.genreNamed(r, libs, id)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	if !ok {
		a.refuse(w, http.StatusNotFound)
		return
	}
	cards, err := a.svc.Catalogue.GenreMix(r.Context(), auth.SessionOf(r.Context()).Profile.ID, name)
	a.writeMix(w, r, cards, err)
}

func (a *API) writeMix(w http.ResponseWriter, r *http.Request, cards []store.Card, err error) {
	if err != nil {
		a.internal(w, r, err)
		return
	}
	l := listedOf(w, r)
	a.writeList(w, r, cards[:min(l.limit, len(cards))], len(cards), 0, l)
}
