package store

import (
	"context"
	"strings"
	"unicode"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
)

// SearchQuery asks for the films, shows, collections and episodes whose title has words starting
// with each word typed, ignoring case and accents: "amel" finds Amélie, once however many libraries
// hold it. Library narrows it to one library. Limit of them are answered from Offset.
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
	matching := `
		FROM items
		WHERE kind IN (@movie, @show, @collection, @episode) AND search @@ to_tsquery('simple', search_text(@query))
			AND (CAST(@library AS uuid) IS NULL OR library_id = CAST(@library AS uuid))
			AND EXISTS (SELECT 1 FROM viewer(CAST(@profile AS uuid)) v WHERE sees(v, items) AND first_of_title(v, items))`
	var library *uuid.UUID
	if q.Library != (uuid.UUID{}) {
		library = &q.Library
	}
	args := pgx.NamedArgs{
		"movie": domain.ItemMovie, "show": domain.ItemShow, "collection": domain.ItemCollection, "episode": domain.ItemEpisode,
		"query": query, "text": q.Text, "library": library, "offset": q.Offset, "limit": q.Limit, "profile": q.Profile,
	}
	var total int64
	if err := s.pool.QueryRow(ctx, `SELECT count(*) `+matching, args).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := queryRows[model.Item](ctx, s.pool, `SELECT `+itemColumns+` `+matching+`
		ORDER BY search_text(title) = search_text(@text) DESC,
			starts_with(search_text(title), search_text(@text)) DESC, kind = @episode,
			ts_rank_cd(search, to_tsquery('simple', search_text(@query))) DESC, sort_title, id
		OFFSET @offset LIMIT @limit`, args)
	if err != nil {
		return nil, 0, err
	}
	cards, err := s.cards(ctx, q.Profile, rows)
	return cards, total, err
}

// PersonRef is someone a search finds, with their picture's id.
type PersonRef struct {
	ID         uuid.UUID
	Name       string
	Photo      uuid.UUID
	Blurhashes Blurhashes
}

// SearchPeople answers limit of the people from offset whose names have a word starting with each
// word asked for, as titles are matched, those whose names start with it first, then the most
// credited; and how many match in all.
func (s *Store) SearchPeople(ctx context.Context, text string, offset, limit int) ([]PersonRef, int64, error) {
	query := prefixes(text)
	if query == "" {
		return []PersonRef{}, 0, nil
	}
	matching := `FROM people p WHERE to_tsvector('simple', search_text(p.name)) @@ to_tsquery('simple', search_text(@query))`
	args := pgx.NamedArgs{"query": query, "text": text, "offset": offset, "limit": limit}
	var total int64
	if err := s.pool.QueryRow(ctx, `SELECT count(*) `+matching, args).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx, `SELECT p.id, p.name, p.photo_id, p.photo_blurhash `+matching+`
		ORDER BY starts_with(search_text(p.name), search_text(@text)) DESC,
			(SELECT count(*) FROM credits c WHERE c.person_id = p.id) DESC, p.name, p.id
		OFFSET @offset LIMIT @limit`, args)
	if err != nil {
		return nil, 0, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (PersonRef, error) {
		var p PersonRef
		var photoID *uuid.UUID
		var hash *string
		err := r.Scan(&p.ID, &p.Name, &photoID, &hash)
		p.Photo, p.Blurhashes = photo(photoID, hash)
		return p, err
	})
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
