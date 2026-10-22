package store

import (
	"math"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// MaxOffset is the furthest a page may start, as far as Jellyfin's StartIndex, an int32, reaches:
// past the end of any library, and so far below the largest int that a page's length added to it
// cannot wrap round.
const MaxOffset = math.MaxInt32

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
