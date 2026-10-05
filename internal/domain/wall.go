package domain

import (
	"fmt"
	"slices"
)

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

// Mark is what a profile has made of a title, which a wall may be narrowed to.
type Mark string

const (
	MarkWatched   Mark = "watched"
	MarkUnwatched Mark = "unwatched"
	// MarkInProgress is a film part watched, or a show some of whose episodes are.
	MarkInProgress Mark = "in_progress"
	MarkFavourite  Mark = "favourite"
)

func Marks() []Mark {
	return []Mark{MarkWatched, MarkUnwatched, MarkInProgress, MarkFavourite}
}

func ParseMark(s string) (Mark, error) {
	if v := Mark(s); slices.Contains(Marks(), v) {
		return v, nil
	}
	return "", fmt.Errorf("mark %q is not one of %v", s, Marks())
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

func ParseResolution(s string) (Resolution, error) {
	if v := Resolution(s); slices.Contains(Resolutions(), v) {
		return v, nil
	}
	return "", fmt.Errorf("resolution %q is not one of %v", s, Resolutions())
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

func ParseRange(s string) (Range, error) {
	if v := Range(s); slices.Contains(Ranges(), v) {
		return v, nil
	}
	return "", fmt.Errorf("range %q is not one of %v", s, Ranges())
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

func ParseWallSort(s string) (WallSort, error) {
	if v := WallSort(s); slices.Contains(WallSorts(), v) {
		return v, nil
	}
	return "", fmt.Errorf("sort %q is not one of %v", s, WallSorts())
}

func Orders() []Order {
	return []Order{Ascending, Descending}
}

func ParseOrder(s string) (Order, error) {
	if v := Order(s); slices.Contains(Orders(), v) {
		return v, nil
	}
	return "", fmt.Errorf("order %q is not %s or %s", s, Ascending, Descending)
}
