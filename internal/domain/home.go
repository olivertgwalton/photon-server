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
	// RowCollection is a row for each collection placed on the home page, by name.
	RowCollection HomeRow = "collection"
)

func HomeRows() []HomeRow {
	return []HomeRow{RowContinueWatching, RowNextUp, RowFavourites, RowRecentFilms, RowRecentShows, RowCollection}
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
