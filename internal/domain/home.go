package domain

// HomeRow is a row of a profile's home page.
type HomeRow string

const (
	// RowContinueWatching is films and episodes stopped part way, the most recent first.
	RowContinueWatching HomeRow = "continue_watching"
	// RowNextUp is, for each show under way, the episode after the last one watched.
	RowNextUp HomeRow = "next_up"
	// RowWatchlist is what the profile put on its watchlist, the latest first.
	RowWatchlist  HomeRow = "watchlist"
	RowFavourites HomeRow = "favourites"
	// RowRecentFilms and RowRecentShows are what was added last; a show by its newest episode.
	RowRecentFilms HomeRow = "recently_added_films"
	RowRecentShows HomeRow = "recently_added_shows"
	// RowRecentlyReleased is films and shows newly out, a show by its newest episode's air date.
	RowRecentlyReleased HomeRow = "recently_released"
	// RowTopRatedUnwatched is the best rated films and shows the profile has not begun.
	RowTopRatedUnwatched HomeRow = "top_rated_unwatched"
)

func HomeRows() []HomeRow {
	return []HomeRow{RowContinueWatching, RowNextUp, RowWatchlist, RowFavourites, RowRecentFilms, RowRecentShows, RowRecentlyReleased, RowTopRatedUnwatched}
}

// LibraryKinds are the kinds of library the row is made once for each of, as Plex's library hubs
// are: none for a row of the profile's own. A title in two libraries is in each one's row.
func (r HomeRow) LibraryKinds() []LibraryKind {
	switch r {
	case RowContinueWatching, RowNextUp, RowWatchlist, RowFavourites:
		return nil
	case RowRecentFilms:
		return []LibraryKind{LibraryMovies, LibraryMixed}
	case RowRecentShows:
		return []LibraryKind{LibraryShows, LibraryMixed}
	case RowRecentlyReleased, RowTopRatedUnwatched:
		return LibraryKinds()
	}
	return nil
}

// WearsCardArt is whether a card on the row is drawn by the picture CardArt chooses: the rows
// about where the profile is, which draw a film or an episode wide rather than by its poster.
func (r HomeRow) WearsCardArt() bool {
	switch r {
	case RowContinueWatching, RowNextUp:
		return true
	case RowWatchlist, RowFavourites, RowRecentFilms, RowRecentShows, RowRecentlyReleased, RowTopRatedUnwatched:
		return false
	}
	return false
}

// RowVisibility is whether a profile's home shows a row.
type RowVisibility string

const (
	RowShown  RowVisibility = "shown"
	RowHidden RowVisibility = "hidden"
)

func RowVisibilities() []RowVisibility {
	return []RowVisibility{RowShown, RowHidden}
}

// HomeSection is a row of a profile's home as it arranged it, as Jellyfin's home sections are.
type HomeSection struct {
	Row        HomeRow
	Visibility RowVisibility
}

// ArrangeHome is every row in the order chosen, a row chosen twice where it was first, and those
// not chosen after them, shown, in their own order: a row the server gains later is shown at the
// foot of a home arranged before it.
func ArrangeHome(chosen []HomeSection) []HomeSection {
	out := make([]HomeSection, 0, len(HomeRows()))
	seen := map[HomeRow]bool{}
	for _, s := range chosen {
		if !seen[s.Row] {
			seen[s.Row] = true
			out = append(out, s)
		}
	}
	for _, r := range HomeRows() {
		if !seen[r] {
			out = append(out, HomeSection{Row: r, Visibility: RowShown})
		}
	}
	return out
}
