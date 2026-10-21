package store

import "github.com/olivertgwalton/photon-server/internal/domain"

// Page is where a page of a list starts, how many it holds at most, and whether it asks how many
// the list holds in all.
type Page struct {
	Offset, Limit int
	Count         domain.Counting
}

// Extent is what a page tells of its list beyond it: whether more follow it, and how many the list
// holds in all where that was asked.
type Extent struct {
	More  bool
	Total *int64
}
