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
)

func WallSorts() []WallSort {
	return []WallSort{SortTitle, SortAdded}
}

// Order is the direction of a sort.
type Order string

const (
	Ascending  Order = "asc"
	Descending Order = "desc"
)

// DefaultOrder is the direction a sort reads in unless asked otherwise: titles from A, the most
// recently added first.
func (s WallSort) DefaultOrder() Order {
	if s == SortAdded {
		return Descending
	}
	return Ascending
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
