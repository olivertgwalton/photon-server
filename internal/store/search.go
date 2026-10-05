package store

import (
	"context"
	"strings"
	"unicode"
	"uuid"

	"gorm.io/gorm/clause"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
)

// SearchQuery asks for the films and shows whose title has words starting with each word typed,
// ignoring case and accents: "amel" finds Amélie. Library narrows it to one library.
type SearchQuery struct {
	Profile uuid.UUID
	Text    string
	Library uuid.UUID
	Limit   int
}

// Search answers the matching titles: one named exactly what was typed, then those starting with
// it, then the closest matches.
func (s *Store) Search(ctx context.Context, q SearchQuery) ([]Card, error) {
	words := strings.FieldsFunc(q.Text, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	if len(words) == 0 {
		return []Card{}, nil
	}
	for i, w := range words {
		words[i] = w + ":*"
	}
	// gen cannot write a full-text match, so this one query is SQL.
	sql := `
		SELECT * FROM items
		WHERE kind IN (@movie, @show, @collection) AND search @@ to_tsquery('simple', search_text(@query))
			AND (CAST(@library AS uuid) IS NULL OR library_id = CAST(@library AS uuid))
			AND visible(id, CAST(@profile AS uuid))
		ORDER BY search_text(title) = search_text(@text) DESC,
			starts_with(search_text(title), search_text(@text)) DESC,
			ts_rank_cd(search, to_tsquery('simple', search_text(@query))) DESC, sort_title, id
		LIMIT @limit`
	var library *model.UUID
	if q.Library != (uuid.UUID{}) {
		library = new(model.UUID(q.Library))
	}
	var rows []*model.Item
	err := s.q.Item.WithContext(ctx).UnderlyingDB().Raw(sql, map[string]any{
		"movie": domain.ItemMovie, "show": domain.ItemShow, "collection": domain.ItemCollection, "query": strings.Join(words, " & "),
		"text": q.Text, "library": library, "limit": q.Limit, "profile": q.Profile.String(),
	}).Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return s.cards(ctx, q.Profile, rows)
}

// PersonRef is someone a search finds, with their picture's id.
type PersonRef struct {
	ID    uuid.UUID
	Name  string
	Photo uuid.UUID
}

// SearchPeople answers the people whose names have a word starting with each word asked for, as
// titles are matched, those whose names start with it first, then the most credited.
func (s *Store) SearchPeople(ctx context.Context, text string, limit int) ([]PersonRef, error) {
	words := strings.FieldsFunc(text, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	if len(words) == 0 {
		return []PersonRef{}, nil
	}
	db := s.q.Person.WithContext(ctx).UnderlyingDB().Table("people p")
	for _, w := range words {
		db = db.Where("(' ' || search_text(p.name)) LIKE '% ' || search_text(?) || '%'", w)
	}
	var rows []struct {
		ID      model.UUID
		Name    string
		PhotoID *model.UUID
	}
	err := db.Select("p.id, p.name, p.photo_id").
		Order(clause.Expr{SQL: "starts_with(search_text(p.name), search_text(?)) DESC", Vars: []any{text}}).
		Order("(SELECT count(*) FROM credits c WHERE c.person_id = p.id) DESC, p.name, p.id").
		Limit(limit).Scan(&rows).Error
	out := make([]PersonRef, len(rows))
	for n, r := range rows {
		out[n] = PersonRef{ID: uuid.UUID(r.ID), Name: r.Name}
		if r.PhotoID != nil {
			out[n].Photo = uuid.UUID(*r.PhotoID)
		}
	}
	return out, err
}
