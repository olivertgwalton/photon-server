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
	Title string          `json:"t,omitzero"`
	Date  time.Time       `json:"d,omitzero"`
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
		var past, tie gen.Condition
		switch p.Sort {
		case domain.SortTitle:
			past, tie = i.SortTitle.Gt(c.Title), i.SortTitle.Eq(c.Title)
			if desc {
				past = i.SortTitle.Lt(c.Title)
			}
		case domain.SortAdded:
			past, tie = i.AddedAt.Gt(c.Date), i.AddedAt.Eq(c.Date)
			if desc {
				past = i.AddedAt.Lt(c.Date)
			}
		case domain.SortReleased:
			past, tie = i.ReleasedAsc.Gt(c.Date), i.ReleasedAsc.Eq(c.Date)
			if desc {
				past, tie = i.ReleasedDesc.Lt(c.Date), i.ReleasedDesc.Eq(c.Date)
			}
		}
		later := i.ID.Gt(model.UUID(c.ID))
		if desc {
			later = i.ID.Lt(model.UUID(c.ID))
		}
		q = q.Where(i.WithContext(ctx).Where(past).Or(tie, later))
	}
	var key field.Expr
	switch {
	case p.Sort == domain.SortAdded:
		key = i.AddedAt
	case p.Sort == domain.SortReleased && desc:
		key = i.ReleasedDesc
	case p.Sort == domain.SortReleased:
		key = i.ReleasedAsc
	default:
		key = i.SortTitle
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
		c := cursor{Sort: p.Sort, Order: p.Order, ID: uuid.UUID(last.ID)}
		switch {
		case p.Sort == domain.SortTitle:
			c.Title = last.SortTitle
		case p.Sort == domain.SortAdded:
			c.Date = last.AddedAt
		case desc:
			c.Date = last.ReleasedDesc
		default:
			c.Date = last.ReleasedAsc
		}
		raw, err := json.Marshal(c)
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
