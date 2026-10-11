package words

import (
	"github.com/olivertgwalton/photon-server/internal/domain"
)

// HomeRow is a kind of home row's name, as its own page and the home's arrangement head it.
func (w Words) HomeRow(k domain.HomeRow) string { return label(rows, k) }

// LibraryRow is how a row of one library's titles is headed, with {library} where the library's
// name goes, "Recently Added in {library}".
func (w Words) LibraryRow(k domain.HomeRow) string { return libraryRows[k] }

var libraryRows = map[domain.HomeRow]string{
	domain.RowRecentFilms: "Recently Added in {library}", domain.RowRecentShows: "Recently Added in {library}",
	domain.RowRecentlyReleased: "Recently Released in {library}", domain.RowTopRatedUnwatched: "Top Rated in {library}",
}
