package store

import (
	"context"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
)

// similarShown is how many similar titles a title's page offers.
const similarShown = 20

// Similar answers the films or shows most like a title, as Plex ranks them: by how many of its
// first three genres, its first director, its first writer and its five top-billed actors they
// share, all counting alike, the newer first on a tie. A title sharing none is left out.
func (s *Store) Similar(ctx context.Context, profile, id uuid.UUID) ([]Card, error) {
	var kind domain.ItemKind
	if err := s.pool.QueryRow(ctx, `SELECT kind FROM items WHERE id = $1`, id).Scan(&kind); err != nil {
		return nil, found(err)
	}
	if kind != domain.ItemMovie && kind != domain.ItemShow {
		return []Card{}, nil
	}
	rows, err := queryRows[model.Item](ctx, s.pool, `
		WITH src AS (
			SELECT id, kind, ARRAY(SELECT jsonb_array_elements_text(coalesce(genres, '[]'))) AS genres FROM items WHERE id = @id
		), src_genres AS (
			SELECT src.genres[1:3] AS genres FROM src
		), picked AS (
			(SELECT person_id, kind FROM credits WHERE item_id = @id AND kind = 'director' ORDER BY position LIMIT 1)
			UNION ALL
			(SELECT person_id, kind FROM credits WHERE item_id = @id AND kind = 'writer' ORDER BY position LIMIT 1)
			UNION ALL
			(SELECT DISTINCT ON (position, person_id) person_id, kind FROM credits
				WHERE item_id = @id AND kind = 'actor' ORDER BY position, person_id LIMIT 5)
		), shared_people AS (
			SELECT other.item_id, count(DISTINCT (other.person_id, other.kind)) AS shared
			FROM picked JOIN credits other ON other.person_id = picked.person_id AND other.kind = picked.kind
			WHERE other.item_id <> @id
			GROUP BY other.item_id
		), candidates AS (
			SELECT i.id, i.released_desc, i.added_at, ARRAY(SELECT jsonb_array_elements_text(coalesce(i.genres, '[]'))) AS genre_list
			FROM items i, src, src_genres g
			WHERE i.kind = src.kind AND (i.genres ?| g.genres OR i.id IN (SELECT item_id FROM shared_people))
				AND i.id NOT IN (SELECT same_title(@id))
				AND EXISTS (SELECT 1 FROM viewer(@profile) v WHERE sees(v, i))
		), ranked AS (
			SELECT c.id, c.released_desc, c.added_at,
				cardinality(ARRAY(SELECT unnest(c.genre_list) INTERSECT SELECT unnest(g.genres))) + coalesce(p.shared, 0) AS score
			FROM candidates c CROSS JOIN src_genres g
			LEFT JOIN shared_people p ON p.item_id = c.id
			ORDER BY score DESC, c.released_desc DESC NULLS LAST, c.added_at, c.id
			-- Kept whole, so only the titles down the ranking until enough are shown are asked
			-- whether they are the one of their title shown, not every candidate.
			OFFSET 0
		), shown AS (
			SELECT id AS shown_id, score FROM ranked
			WHERE (SELECT NOT EXISTS (SELECT 1 FROM seen_before(v, i)) FROM items i, viewer(@profile) v WHERE i.id = ranked.id)
			LIMIT @limit
		)
		SELECT `+itemColumns+` FROM shown JOIN items ON items.id = shown.shown_id
		ORDER BY shown.score DESC, items.released_desc DESC NULLS LAST, items.added_at, items.id`,
		pgx.NamedArgs{"id": id, "limit": similarShown, "profile": profile})
	if err != nil {
		return nil, err
	}
	return s.cards(ctx, profile, rows)
}
