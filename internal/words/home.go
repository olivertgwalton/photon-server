package words

import (
	"cmp"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// Row is a kind of home row's name, as its own page heads it: "Continue Watching".
func (w Words) Row(k domain.HomeRow) string {
	return cmp.Or(rows[k], string(k))
}

var rows = map[domain.HomeRow]string{
	domain.RowContinueWatching: "Continue Watching", domain.RowNextUp: "Next Up", domain.RowWatchlist: "Watchlist",
	domain.RowFavourites: "Favourites", domain.RowRecentFilms: "Recently Added Films",
	domain.RowRecentShows: "Recently Added Shows", domain.RowRecentlyReleased: "Recently Released",
	domain.RowTopRatedUnwatched: "Top Rated", domain.RowCollection: "Collections",
}

// HomeRow is a home row's heading: a collection's by the collection's name, and a row of one
// library's titles by what it holds and the library, "Recently Added in Films".
func (w Words) HomeRow(k domain.HomeRow, library, collection string) string {
	if k == domain.RowCollection && collection != "" {
		return collection
	}
	if of, ok := libraryRows[k]; ok && library != "" {
		return of + " in " + library
	}
	return w.Row(k)
}

// What a row of one library's titles holds, before the library's name.
var libraryRows = map[domain.HomeRow]string{
	domain.RowRecentFilms: "Recently Added", domain.RowRecentShows: "Recently Added",
	domain.RowRecentlyReleased: "Recently Released", domain.RowTopRatedUnwatched: "Top Rated",
}
