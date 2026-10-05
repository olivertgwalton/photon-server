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
)

func WallSorts() []WallSort {
	return []WallSort{SortTitle, SortAdded, SortReleased}
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

func ParseOrder(s string) (Order, error) {
	if v := Order(s); v == Ascending || v == Descending {
		return v, nil
	}
	return "", fmt.Errorf("order %q is not %s or %s", s, Ascending, Descending)
}
