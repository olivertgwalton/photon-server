package store

import (
	"context"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/store/model"
)

// BookPage is what only an author's or a book's page has: a book's author, and the series it is in
// and its place there; an author's books.
type BookPage struct {
	Author      *TitleRef
	Series      string
	SeriesIndex *float64
	Books       []Card
}

// bookOrder orders an author's books, of alias, by series and their place in it, as a shelf keeps
// them, and a book in no series by when it came out.
func bookOrder(alias string) string {
	return alias + ".series NULLS LAST, " + alias + ".series_index NULLS LAST, " + alias + ".released_asc, " + alias + ".sort_title, " + alias + ".id"
}

// Books answers an author's books as cards, in their order.
func (s *Store) Books(ctx context.Context, profile, author uuid.UUID) ([]Card, error) {
	rows, err := queryRows[model.Item](ctx, s.pool, `
		SELECT `+itemColumns+` FROM items WHERE parent_id = $1 AND kind = 'book' ORDER BY `+bookOrder("items"), author)
	if err != nil {
		return nil, err
	}
	return s.cards(ctx, profile, rows)
}
