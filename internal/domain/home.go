package domain

// HomeRow is a row of a profile's home page.
type HomeRow string

const (
	// RowContinueWatching is films and episodes stopped part way, the most recent first.
	RowContinueWatching HomeRow = "continue_watching"
	// RowNextUp is, for each show under way, the episode after the last one watched.
	RowNextUp     HomeRow = "next_up"
	RowFavourites HomeRow = "favourites"
	// RowRecentFilms and RowRecentShows are what was added last; a show by its newest episode.
	RowRecentFilms HomeRow = "recently_added_films"
	RowRecentShows HomeRow = "recently_added_shows"
)

func HomeRows() []HomeRow {
	return []HomeRow{RowContinueWatching, RowNextUp, RowFavourites, RowRecentFilms, RowRecentShows}
}
