package store

import (
	"context"
	"strings"
	"unicode"
	"unicode/utf8"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
)

// SearchQuery asks for the films, shows, collections and episodes whose title has words starting
// with each word typed, ignoring case and accents: "amel" finds Amélie, once however many libraries
// hold it. Library narrows it to one library, and Kinds to those kinds of title. Limit of them are
// answered from Offset.
type SearchQuery struct {
	Profile uuid.UUID
	Text    string
	Library uuid.UUID
	Kinds   []domain.ItemKind
	Offset  int
	Limit   int
	Count   domain.Counting
}

// nearFrom is how many letters and digits are typed before titles near what was typed are
// looked for: fewer share trigrams with too many titles to mean anything.
const nearFrom = 5

// nearEnough is the word similarity, pg_trgm's, a title must have to what was typed to be found
// as near it, set as each connection opens: a letter wrong in a word of five or six scores 0.4,
// and a title that merely shares a few letters scores under it.
const nearEnough = "0.4"

// searchedText is the text a title is searched by, as its search column and trigram index are made
// of it.
const searchedText = `search_text(title || ' ' || coalesce(original_title, ''))`

// Search answers a page of the matching titles, and what the page tells of the rest: one named
// exactly what was typed, then those starting with it, then the closest matches; an episode after
// the films, shows and collections matched as well, as Jellyfin ranks them. Where no title has the
// words typed, those typed with a letter wrong are found by the trigrams they share, the nearest
// first, as Plex finds them.
func (s *Store) Search(ctx context.Context, q SearchQuery) ([]Card, Extent, error) {
	words := words(q.Text)
	if len(words) == 0 {
		return []Card{}, nothingFound(q.Count), nil
	}
	kinds := q.Kinds
	if len(kinds) == 0 {
		kinds = []domain.ItemKind{domain.ItemMovie, domain.ItemShow, domain.ItemCollection, domain.ItemEpisode}
	}
	args := pgx.NamedArgs{
		"kinds": kinds, "episode": domain.ItemEpisode,
		"query": prefixes(words), "text": q.Text, "library": optional(q.Library), "offset": q.Offset, "limit": q.Limit + 1, "profile": q.Profile,
	}
	const typed = `search @@ to_tsquery('simple', search_text(@query))`
	page := Page{Offset: q.Offset, Limit: q.Limit, Count: q.Count}
	cards, extent, err := s.titlesMatching(ctx, q.Profile, args, page, typed,
		`search_text(title) = search_text(@text) DESC, starts_with(search_text(title), search_text(@text)) DESC,
			kind = @episode, ts_rank_cd(search, to_tsquery('simple', search_text(@query))) DESC`)
	if err != nil || len(cards) > 0 || utf8.RuneCountInString(strings.Join(words, "")) < nearFrom {
		return cards, extent, err
	}
	// A page past the first that is empty may be past every title with the words typed.
	if q.Offset > 0 {
		var typedSome bool
		if err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 `+titlesWhere(typed)+`)`, args).Scan(&typedSome); err != nil || typedSome {
			return cards, extent, err
		}
	}
	return s.titlesMatching(ctx, q.Profile, args, page, `search_text(@text) <% `+searchedText,
		`word_similarity(search_text(@text), `+searchedText+`) DESC, kind = @episode`)
}

// titlesWhere is the FROM and WHERE of the titles args asks for among those matching where.
func titlesWhere(where string) string {
	return `
		FROM items, viewer(CAST(@profile AS uuid)) v
		WHERE kind = ANY(@kinds) AND ` + where + `
			AND (CAST(@library AS uuid) IS NULL OR library_id = CAST(@library AS uuid)
				OR kind = 'collection' AND ` + holdsTitleOf("CAST(@library AS uuid)") + `)
			AND (kind <> 'collection' OR id IN (` + shownCollections + `))
			AND sees(v, items) AND NOT EXISTS (SELECT 1 FROM seen_before(v, items))`
}

// titlesMatching answers the page of titles args asks for among those matching where, in the
// order given, and what the page tells of the rest.
func (s *Store) titlesMatching(ctx context.Context, profile uuid.UUID, args pgx.NamedArgs, page Page, where, order string) ([]Card, Extent, error) {
	matching := titlesWhere(where)
	var rows []*model.Item
	total, err := s.counted(ctx, page.Count, `SELECT count(*) `+matching, args, func(ctx context.Context) (err error) {
		rows, err = queryRows[model.Item](ctx, s.pool, `SELECT `+itemColumns+` `+matching+`
			ORDER BY `+order+`, sort_title, id
			OFFSET @offset LIMIT @limit`, args)
		return err
	})
	if err != nil {
		return nil, Extent{}, err
	}
	rows, more := pageOf(rows, page.Limit)
	cards, err := s.cards(ctx, profile, rows)
	return cards, Extent{More: more, Total: total}, err
}

// PersonRef is someone a search finds, with their picture's id.
type PersonRef struct {
	ID         uuid.UUID
	Name       string
	Photo      uuid.UUID
	Blurhashes Blurhashes
}

// SearchPeople answers a page of the people whose names have a word starting with each word asked
// for, as titles are matched, those whose names start with it first, then the most credited; and
// what the page tells of the rest.
func (s *Store) SearchPeople(ctx context.Context, text string, page Page) ([]PersonRef, Extent, error) {
	words := words(text)
	if len(words) == 0 {
		return []PersonRef{}, nothingFound(page.Count), nil
	}
	matching := `FROM people p WHERE to_tsvector('simple', search_text(p.name)) @@ to_tsquery('simple', search_text(@query))`
	args := pgx.NamedArgs{"query": prefixes(words), "text": text, "offset": page.Offset, "limit": page.Limit + 1}
	var out []PersonRef
	total, err := s.counted(ctx, page.Count, `SELECT count(*) `+matching, args, func(ctx context.Context) error {
		rows, err := s.pool.Query(ctx, `SELECT p.id, p.name, p.photo_id, p.photo_blurhash `+matching+`
			ORDER BY starts_with(search_text(p.name), search_text(@text)) DESC,
				(SELECT count(*) FROM credits c WHERE c.person_id = p.id) DESC, p.name, p.id
			OFFSET @offset LIMIT @limit`, args)
		if err != nil {
			return err
		}
		out, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (PersonRef, error) {
			var p PersonRef
			var photoID *uuid.UUID
			var hash *string
			err := r.Scan(&p.ID, &p.Name, &photoID, &hash)
			p.Photo, p.Blurhashes = photo(photoID, hash)
			return p, err
		})
		return err
	})
	out, more := pageOf(out, page.Limit)
	return out, Extent{More: more, Total: total}, err
}

// nothingFound is what an empty search tells of the rest: nothing, and that none were found where
// asked how many.
func nothingFound(counting domain.Counting) Extent {
	if counting != domain.CountExact {
		return Extent{}
	}
	return Extent{Total: new(int64(0))}
}

// words are the words typed: runs of letters and digits.
func words(text string) []string {
	return strings.FieldsFunc(text, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

// prefixes is a full-text query for every word typed as the start of a word.
func prefixes(words []string) string {
	starts := make([]string, len(words))
	for i, w := range words {
		starts[i] = w + ":*"
	}
	return strings.Join(starts, " & ")
}
