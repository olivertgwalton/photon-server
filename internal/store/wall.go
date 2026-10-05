package store

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"
	"uuid"

	"gorm.io/gen"
	"gorm.io/gen/field"
	"gorm.io/gorm"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
)

var ErrBadCursor = errors.New("the cursor is not one this listing gave")

// Card is a title as a wall shows it.
type Card struct {
	ID          uuid.UUID
	Kind        domain.ItemKind
	Title       string
	Year        int
	ReleaseDate time.Time
	AddedAt     time.Time
}

// WallPage asks for one page of a library's titles. After is the cursor the previous page
// answered, empty for the first.
type WallPage struct {
	Sort  domain.WallSort
	Order domain.Order
	After string
	Limit int
}

// cursor is the last title a page held, by the key it was sorted on.
type cursor struct {
	Sort  domain.WallSort `json:"s"`
	Order domain.Order    `json:"o"`
	Title string          `json:"t"`
	Added time.Time       `json:"a"`
	ID    uuid.UUID       `json:"i"`
}

// Wall answers a page of a library's films or shows and the cursor for the next, empty after the
// last. Paging by key, not offset, keeps a page stable while titles are added before it.
func (s *Store) Wall(ctx context.Context, lib uuid.UUID, p WallPage) ([]Card, string, error) {
	l := s.q.Library
	if _, err := l.WithContext(ctx).Where(l.ID.Eq(model.UUID(lib))).Take(); errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, "", ErrNotFound
	} else if err != nil {
		return nil, "", err
	}
	i := s.q.Item
	q := i.WithContext(ctx).Where(i.LibraryID.Eq(model.UUID(lib)), i.Kind.In(string(domain.ItemMovie), string(domain.ItemShow)))
	desc := p.Order == domain.Descending
	if p.After != "" {
		var c cursor
		raw, err := base64.RawURLEncoding.DecodeString(p.After)
		if err != nil || json.Unmarshal(raw, &c) != nil || c.Sort != p.Sort || c.Order != p.Order {
			return nil, "", ErrBadCursor
		}
		var past, tie gen.Condition = i.SortTitle.Gt(c.Title), i.SortTitle.Eq(c.Title)
		switch {
		case p.Sort == domain.SortTitle && desc:
			past = i.SortTitle.Lt(c.Title)
		case p.Sort == domain.SortAdded && desc:
			past, tie = i.AddedAt.Lt(c.Added), i.AddedAt.Eq(c.Added)
		case p.Sort == domain.SortAdded:
			past, tie = i.AddedAt.Gt(c.Added), i.AddedAt.Eq(c.Added)
		}
		later := i.ID.Gt(model.UUID(c.ID))
		if desc {
			later = i.ID.Lt(model.UUID(c.ID))
		}
		q = q.Where(i.WithContext(ctx).Where(past).Or(tie, later))
	}
	var key field.Expr = i.SortTitle
	if p.Sort == domain.SortAdded {
		key = i.AddedAt
	}
	if desc {
		q = q.Order(key.Desc(), i.ID.Desc())
	} else {
		q = q.Order(key, i.ID)
	}
	rows, err := q.Limit(p.Limit + 1).Find()
	if err != nil {
		return nil, "", err
	}
	var next string
	if len(rows) > p.Limit {
		rows = rows[:p.Limit]
		last := rows[len(rows)-1]
		raw, err := json.Marshal(cursor{
			Sort: p.Sort, Order: p.Order, Title: last.SortTitle, Added: last.AddedAt, ID: uuid.UUID(last.ID),
		})
		if err != nil {
			return nil, "", err
		}
		next = base64.RawURLEncoding.EncodeToString(raw)
	}
	cards := make([]Card, len(rows))
	for n, r := range rows {
		cards[n] = Card{ID: uuid.UUID(r.ID), Kind: r.Kind, Title: r.Title, AddedAt: r.AddedAt}
		if r.Year != nil {
			cards[n].Year = *r.Year
		}
		if r.ReleaseDate != nil {
			cards[n].ReleaseDate = *r.ReleaseDate
		}
	}
	return cards, next, nil
}
