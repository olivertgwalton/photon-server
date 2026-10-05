package store

import (
	"context"
	"strings"
	"unicode"
	"uuid"

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
		WHERE kind IN (@movie, @show) AND search @@ to_tsquery('simple', search_text(@query))
			AND (CAST(@library AS uuid) IS NULL OR library_id = CAST(@library AS uuid))
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
		"movie": domain.ItemMovie, "show": domain.ItemShow, "query": strings.Join(words, " & "),
		"text": q.Text, "library": library, "limit": q.Limit,
	}).Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return s.cards(ctx, q.Profile, rows)
}
