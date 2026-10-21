package store

import (
	"time"
	"uuid"
)

// Before is where a list read newest first goes on from: the time and id of the last entry a page
// held, those after it being what came before. A page is found from it in the index the list is
// kept in order by, however far down it is, and an entry added meanwhile moves none onto the next.
type Before struct {
	At time.Time
	ID uuid.UUID
}

// Newest is where a list's first page goes on from: after anything there could be.
var Newest = Before{At: time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC), ID: uuid.Max()}

// pageOf is a page of up to limit of rows, read as limit and one more, and whether there were more.
func pageOf[T any](rows []T, limit int) ([]T, bool) {
	if len(rows) > limit {
		return rows[:limit], true
	}
	return rows, false
}
