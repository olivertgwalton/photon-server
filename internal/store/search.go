package store

import (
	"context"
	"strings"
	"unicode"
	"uuid"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
)

// SearchQuery asks for the films, shows, collections and episodes whose title has words starting
// with each word typed, ignoring case and accents: "amel" finds Amélie. Library narrows it to one
// library. Limit of them are answered from Offset.
type SearchQuery struct {
	Profile uuid.UUID
	Text    string
	Library uuid.UUID
	Offset  int
	Limit   int
}

// Search answers a page of the matching titles, and how many match in all: one named exactly what
// was typed, then those starting with it, then the closest matches; an episode after the films,
// shows and collections matched as well, as Jellyfin ranks them.
func (s *Store) Search(ctx context.Context, q SearchQuery) ([]Card, int64, error) {
	query := prefixes(q.Text)
	if query == "" {
		return []Card{}, 0, nil
	}
	// gen cannot write a full-text match, so this one query is SQL.
	matching := `
		FROM items
		WHERE kind IN (@movie, @show, @collection, @episode) AND search @@ to_tsquery('simple', search_text(@query))
			AND (CAST(@library AS uuid) IS NULL OR library_id = CAST(@library AS uuid))
			AND EXISTS (SELECT 1 FROM viewer(CAST(@profile AS uuid)) v WHERE sees(v, items))`
	var library *model.UUID
	if q.Library != (uuid.UUID{}) {
		library = new(model.UUID(q.Library))
	}
	args := map[string]any{
		"movie": domain.ItemMovie, "show": domain.ItemShow, "collection": domain.ItemCollection, "episode": domain.ItemEpisode,
		"query": query, "text": q.Text, "library": library, "offset": q.Offset, "limit": q.Limit, "profile": q.Profile.String(),
	}
	db := s.q.Item.WithContext(ctx).UnderlyingDB()
	var total int64
	if err := db.Raw(`SELECT count(*) `+matching, args).Scan(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []*model.Item
	err := db.Raw(`SELECT * `+matching+`
		ORDER BY search_text(title) = search_text(@text) DESC,
			starts_with(search_text(title), search_text(@text)) DESC, kind = @episode,
			ts_rank_cd(search, to_tsquery('simple', search_text(@query))) DESC, sort_title, id
		OFFSET @offset LIMIT @limit`, args).Find(&rows).Error
	if err != nil {
		return nil, 0, err
	}
	cards, err := s.cards(ctx, q.Profile, rows)
	return cards, total, err
}

// PersonRef is someone a search finds, with their picture's id.
type PersonRef struct {
	ID    uuid.UUID
	Name  string
	Photo uuid.UUID
}

// SearchPeople answers limit of the people from offset whose names have a word starting with each
// word asked for, as titles are matched, those whose names start with it first, then the most
// credited; and how many match in all.
func (s *Store) SearchPeople(ctx context.Context, text string, offset, limit int) ([]PersonRef, int64, error) {
	query := prefixes(text)
	if query == "" {
		return []PersonRef{}, 0, nil
	}
	db := s.q.Person.WithContext(ctx).UnderlyingDB().Table("people p").
		Where("to_tsvector('simple', search_text(p.name)) @@ to_tsquery('simple', search_text(?))", query)
	var total int64
	if err := db.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []struct {
		ID      model.UUID
		Name    string
		PhotoID *model.UUID
	}
	err := db.Select("p.id, p.name, p.photo_id").
		Order(clause.Expr{SQL: "starts_with(search_text(p.name), search_text(?)) DESC", Vars: []any{text}}).
		Order("(SELECT count(*) FROM credits c WHERE c.person_id = p.id) DESC, p.name, p.id").
		Offset(offset).Limit(limit).Scan(&rows).Error
	out := make([]PersonRef, len(rows))
	for n, r := range rows {
		out[n] = PersonRef{ID: uuid.UUID(r.ID), Name: r.Name}
		if r.PhotoID != nil {
			out[n].Photo = uuid.UUID(*r.PhotoID)
		}
	}
	return out, total, err
}

// prefixes is a full-text query for every word typed as the start of a word, "" for no words.
func prefixes(text string) string {
	words := strings.FieldsFunc(text, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	for i, w := range words {
		words[i] = w + ":*"
	}
	return strings.Join(words, " & ")
}
