package words

import (
	"github.com/olivertgwalton/photon-server/internal/domain"
)

// HomeRow is a home row's heading: a collection's by the collection's name, and a row of one
// library's titles by what it holds and the library, "Recently Added in Films".
func (w Words) HomeRow(k domain.HomeRow, library, collection string) string {
	if k == domain.RowCollection && collection != "" {
		return collection
	}
	if of, ok := libraryRows[k]; ok && library != "" {
		return of + " in " + library
	}
	return label(rows, k)
}

// What a row of one library's titles holds, before the library's name.
var libraryRows = map[domain.HomeRow]string{
	domain.RowRecentFilms: "Recently Added", domain.RowRecentShows: "Recently Added",
	domain.RowRecentlyReleased: "Recently Released", domain.RowTopRatedUnwatched: "Top Rated",
}
