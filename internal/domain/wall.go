package domain

import "slices"

// WallSort is an order a library's titles can be paged in.
type WallSort string

const (
	SortTitle WallSort = "title"
	SortAdded WallSort = "added"
	// SortReleased is by release date, else the year; a title with neither comes last.
	SortReleased WallSort = "released"
	// SortRating is by one site's rating; a title it has not rated comes last.
	SortRating WallSort = "rating"
	// SortRuntime is by how long a film's longest copy runs.
	SortRuntime WallSort = "runtime"
	// SortPlayed is by when the profile last played it; a title never played comes last.
	SortPlayed WallSort = "played"
)

func WallSorts() []WallSort {
	return []WallSort{SortTitle, SortAdded, SortReleased, SortRating, SortRuntime, SortPlayed}
}

// WallKind is what a wall pages: a library's titles, its films or its shows alone, as Jellyfin's
// includeItemTypes narrows a mixed library, or its episodes across its shows, as Plex's episode
// type and Jellyfin's Episodes tab list them.
type WallKind string

const (
	WallTitles   WallKind = "titles"
	WallFilms    WallKind = "films"
	WallShows    WallKind = "shows"
	WallEpisodes WallKind = "episodes"
)

func WallKinds() []WallKind {
	return []WallKind{WallTitles, WallFilms, WallShows, WallEpisodes}
}

// Of is the kinds of item a wall of a library of kind lists.
func (w WallKind) Of(lib LibraryKind) []ItemKind {
	held := lib.ItemKinds()
	var asked []ItemKind
	switch w {
	case WallTitles:
		return lib.Titles()
	case WallFilms:
		asked = []ItemKind{ItemMovie}
	case WallShows:
		asked = []ItemKind{ItemShow}
	case WallEpisodes:
		asked = []ItemKind{ItemEpisode}
	}
	return slices.DeleteFunc(asked, func(k ItemKind) bool { return !slices.Contains(held, k) })
}

// Mark is what a profile has made of a title, which a wall may be narrowed to.
type Mark string

const (
	MarkWatched   Mark = "watched"
	MarkUnwatched Mark = "unwatched"
	// MarkInProgress is a film part watched, or a show some of whose episodes are.
	MarkInProgress Mark = "in_progress"
	MarkFavourite  Mark = "favourite"
	MarkWatchlist  Mark = "watchlist"
)

func Marks() []Mark {
	return []Mark{MarkWatched, MarkUnwatched, MarkInProgress, MarkFavourite, MarkWatchlist}
}

// Resolution is a picture's size class, by its width: a scope master is no less 4K for being
// short.
type Resolution string

const (
	ResolutionSD  Resolution = "sd"
	ResolutionHD  Resolution = "720p"
	ResolutionFHD Resolution = "1080p"
	ResolutionUHD Resolution = "4k"
)

func Resolutions() []Resolution {
	return []Resolution{ResolutionSD, ResolutionHD, ResolutionFHD, ResolutionUHD}
}

// ResolutionOf is the resolution a picture this wide is filed under, as a wall's filter files it.
func ResolutionOf(width int) Resolution {
	for _, r := range Resolutions() {
		if from, to := r.Widths(); width >= from && (to == 0 || width < to) {
			return r
		}
	}
	return ResolutionSD
}

// Widths are the widths a resolution spans, the upper bound excluded and zero for none.
func (r Resolution) Widths() (from, to int) {
	switch r {
	case ResolutionSD:
		return 0, 1200
	case ResolutionHD:
		return 1200, 1800
	case ResolutionFHD:
		return 1800, 3200
	case ResolutionUHD:
		return 3200, 0
	}
	return 0, 0
}

// Order is the direction of a sort.
type Order string

const (
	Ascending  Order = "asc"
	Descending Order = "desc"
)

// DefaultOrder is the direction a sort reads in unless asked otherwise: titles from A, the newest
// first by date.
func (s WallSort) DefaultOrder() Order {
	if s == SortTitle {
		return Ascending
	}
	return Descending
}

func Orders() []Order {
	return []Order{Ascending, Descending}
}
