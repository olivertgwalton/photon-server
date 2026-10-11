package httpapi

import (
	"net/http"
	"slices"
	"strconv"

	"github.com/olivertgwalton/photon-server/internal/auth"
	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// defaultRowLimit is as many titles as a row holds, as Plex's and Jellyfin's home rows do.
const defaultRowLimit = 20

// profileRow answers a page of one of the profile's own rows, in the row's order.
func (a *API) profileRow(w http.ResponseWriter, r *http.Request) {
	offset, limit, ok := a.paging(w, r, defaultWallLimit)
	if !ok {
		return
	}
	row := domain.HomeRow(r.PathValue("row"))
	cards, total, err := a.svc.Catalogue.RowPage(r.Context(), auth.SessionOf(r.Context()).Profile.ID, row, offset, limit)
	if a.answered(w, r, err) {
		return
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, pageJSON[cardJSON]{cardsJSON(cards), offset, total})
}

// libraryRow answers the first of one of a library's rows, as the home page has it: the newest or
// the best, a count and no more, as Plex's and Jellyfin's are.
func (a *API) libraryRow(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r, "id")
	if !ok {
		return
	}
	// The route takes no offset.
	_, limit, ok := a.paging(w, r, defaultRowLimit)
	if !ok {
		return
	}
	profile := auth.SessionOf(r.Context()).Profile.ID
	libs, err := a.svc.Catalogue.LibrariesSeen(r.Context(), profile)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	i := slices.IndexFunc(libs, func(l *store.SeenLibrary) bool { return l.ID == id })
	if i < 0 {
		a.answered(w, r, store.ErrNotFound)
		return
	}
	lib, row := libs[i], domain.HomeRow(r.PathValue("row"))
	cards, err := a.svc.Catalogue.LibraryRow(r.Context(), profile, row, lib, limit)
	if a.answered(w, r, err) {
		return
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, listJSON[cardJSON]{Items: cardsJSON(cards)})
}

func (a *API) rowsRoutes() []route {
	return []route{
		{
			pattern: "GET /api/v1/profile/rows/{row}", access: signedIn,
			summary: "Page one of the profile's own rows: continue watching, next up, its watchlist or its favourites",
			path:    []param{{"row", domain.HomeRow(""), "The row; a library's is asked of the library."}},
			query:   pageParams, status: http.StatusOK, reply: pageJSON[cardJSON]{}, handle: a.profileRow,
		},
		{
			pattern: "GET /api/v1/libraries/{id}/rows/{row}", access: signedIn,
			summary: "The first of one of a library's rows: recently added, recently released or top rated",
			path:    []param{{"row", domain.HomeRow(""), "The row, made for the library's kind."}},
			query:   []param{{"limit", 0, "How many titles, from 1 to " + strconv.Itoa(maxWallLimit) + "."}},
			status:  http.StatusOK, reply: listJSON[cardJSON]{}, handle: a.libraryRow,
		},
	}
}
