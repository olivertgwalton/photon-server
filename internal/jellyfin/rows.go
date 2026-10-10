package jellyfin

import (
	"errors"
	"net/http"
	"slices"
	"strings"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/auth"
	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// rowLimit is the most of a row an app is given at once, and latestLimit how many of what was added last
// where it asks for no limit, as Jellyfin gives.
const (
	rowLimit    = 50
	latestLimit = 20
)

// row answers a page of one of the profile's rows, such as Continue Watching, as a list.
func (a *API) row(kind domain.HomeRow) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		l := listedOf(w, r)
		cards, total, err := a.svc.Catalogue.RowPage(r.Context(), auth.SessionOf(r.Context()).Profile.ID, kind, l.start, min(l.limit, rowLimit))
		if err != nil {
			a.internal(w, r, err)
			return
		}
		a.writeList(w, r, cards, int(total), l.start, l)
	}
}

// resume answers what the profile is part way through, all of it video: an app asking for audio or
// books alone, as Jellyfin's web app does for its Continue Listening and Reading rows, has none.
func (a *API) resume(w http.ResponseWriter, r *http.Request) {
	types := values(r, "mediaTypes")
	if len(types) > 0 && !slices.ContainsFunc(types, func(t string) bool { return strings.EqualFold(t, "Video") }) {
		l := listedOf(w, r)
		a.writeJSON(w, queryResult{Items: []item{}, StartIndex: l.start})
		return
	}
	a.row(domain.RowContinueWatching)(w, r)
}

// nextUp answers the episodes to play next: of every show, or of the one an app names.
func (a *API) nextUp(w http.ResponseWriter, r *http.Request) {
	show, err := uuid.Parse(query(r, "seriesId"))
	if err != nil {
		a.row(domain.RowNextUp)(w, r)
		return
	}
	var cards []store.Card
	switch c, err := a.svc.Catalogue.Next(r.Context(), auth.SessionOf(r.Context()).Profile.ID, show); {
	case err == nil:
		cards = []store.Card{c}
	case errors.Is(err, store.ErrNotFound), errors.Is(err, store.ErrNoNext):
	default:
		a.internal(w, r, err)
		return
	}
	a.writeList(w, r, cards, len(cards), 0, listedOf(w, r))
}

// latestRows are the rows of what was added last to each kind of library.
var latestRows = map[domain.LibraryKind]domain.HomeRow{domain.LibraryMovies: domain.RowRecentFilms, domain.LibraryShows: domain.RowRecentShows}

// latest answers what was added last to a library, or to every library: a bare list, as Jellyfin's
// is, of films, and of shows by their newest episode.
func (a *API) latest(w http.ResponseWriter, r *http.Request) {
	libs, seen, err := a.seenLibraries(r)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	if parent, err := uuid.Parse(query(r, "parentId")); err == nil {
		libs = nil
		if lib, ok := seen[parent]; ok {
			libs = []*store.SeenLibrary{lib}
		}
	}
	l := listedOf(w, r)
	limit := latestLimit
	if query(r, "limit") != "" {
		limit = min(l.limit, rowLimit)
	}
	var cards []store.Card
	for _, lib := range libs {
		got, err := a.svc.Catalogue.LibraryRow(r.Context(), auth.SessionOf(r.Context()).Profile.ID, latestRows[lib.Kind], lib.ID, limit)
		if err != nil {
			a.internal(w, r, err)
			return
		}
		cards = append(cards, got...)
	}
	items, err := a.list(r.Context(), cards, l)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	a.writeJSON(w, items)
}
