package httpapi

import (
	"net/http"
	"slices"
	"strconv"
	"strings"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/auth"
	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
	"github.com/olivertgwalton/photon-server/internal/words"
)

const defaultHomeLimit = 20

type homeRowJSON struct {
	Kind domain.HomeRow `json:"kind"`
	// Title is the row's heading, in the reader's language.
	Title string `json:"title"`
	// Collection is the collection a row of kind collection is.
	Collection *titleRefJSON `json:"collection,omitzero"`
	// Library is the library a row of its titles is of: recently added, recently released and top
	// rated are a row for each library.
	Library *libraryRefJSON `json:"library,omitzero"`
	Items   []cardJSON      `json:"items"`
}

type libraryRefJSON struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

type homeJSON struct {
	Rows []homeRowJSON `json:"rows"`
}

// home answers the profile's home page: its rows with anything in them, in the order to show.
func (a *API) home(w http.ResponseWriter, r *http.Request) {
	// The route takes no offset.
	_, limit, ok := a.paging(w, r, defaultHomeLimit)
	if !ok {
		return
	}
	rows, err := a.svc.Catalogue.Home(r.Context(), auth.SessionOf(r.Context()).Profile.ID, limit)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	said := words.Negotiate(w, r)
	out := homeJSON{Rows: make([]homeRowJSON, len(rows))}
	for i, row := range rows {
		title := said.HomeRow(row.Kind)
		switch {
		case row.Collection != nil:
			title = row.Collection.Title
		case row.Library != nil:
			title = strings.ReplaceAll(said.LibraryRow(row.Kind), "{library}", row.Library.Name)
		}
		out.Rows[i] = homeRowJSON{
			Kind: row.Kind, Title: title, Collection: (*titleRefJSON)(row.Collection),
			Library: (*libraryRefJSON)(row.Library), Items: cardsJSON(row.Cards),
		}
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, out)
}

// homeRow answers a page of one of the profile's own home rows, in the row's order.
func (a *API) homeRow(w http.ResponseWriter, r *http.Request) {
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
	_, limit, ok := a.paging(w, r, defaultHomeLimit)
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

func (a *API) homeRoutes() []route {
	return []route{
		{
			pattern: "GET /api/v1/home", access: signedIn, summary: "The profile's home rows, in order",
			query:  []param{{"limit", 0, "How many titles a row holds, from 1 to " + strconv.Itoa(maxWallLimit) + "."}},
			status: http.StatusOK, reply: homeJSON{}, handle: a.home,
		},
		{
			pattern: "GET /api/v1/home/{row}", access: signedIn,
			summary: "Page one of the profile's own home rows: continue watching, next up, its watchlist or its favourites",
			path:    []param{{"row", domain.HomeRow(""), "The row; a library's or a collection's is paged on its own page."}},
			query:   pageParams, status: http.StatusOK, reply: pageJSON[cardJSON]{}, handle: a.homeRow,
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
