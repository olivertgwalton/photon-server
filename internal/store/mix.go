package store

import (
	"context"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/olivertgwalton/photon-server/internal/store/model"
)

// mixShown is how many tracks a mix holds, as Jellyfin's instant mix.
const mixShown = 200

// Mix answers tracks to play from a track, an album or an artist, as Jellyfin mixes them: tracks
// of any genre it or its tracks have, in a random order, a track itself first. Anything else, or
// music of no genre but a track, mixes none.
func (s *Store) Mix(ctx context.Context, profile, id uuid.UUID) ([]Card, error) {
	var music bool
	var genres []string
	err := s.pool.QueryRow(ctx, `
		SELECT kind IN ('artist', 'album', 'track'), ARRAY(
			SELECT DISTINCT g FROM (
				SELECT genres FROM items WHERE id = $1
				UNION ALL
				SELECT genres FROM items WHERE parent_id = $1
				UNION ALL
				SELECT t.genres FROM items a JOIN items t ON t.parent_id = a.id WHERE a.parent_id = $1
			) seed, jsonb_array_elements_text(coalesce(seed.genres, '[]')) g)
		FROM items WHERE id = $1`, id).Scan(&music, &genres)
	if err != nil {
		return nil, found(err)
	}
	if !music {
		return []Card{}, nil
	}
	return s.mix(ctx, profile, id, genres)
}

// GenreMix answers tracks of a genre in a random order, as Jellyfin mixes a genre.
func (s *Store) GenreMix(ctx context.Context, profile uuid.UUID, genre string) ([]Card, error) {
	return s.mix(ctx, profile, uuid.UUID{}, []string{genre})
}

// mix answers the tracks the profile sees of any of the genres, at random, the seed first should
// it be a track.
func (s *Store) mix(ctx context.Context, profile, seed uuid.UUID, genres []string) ([]Card, error) {
	rows, err := queryRows[model.Item](ctx, s.pool, `
		SELECT `+itemColumns+` FROM items
		WHERE kind = 'track' AND (id = @seed OR genres ?| @genres::text[])
			AND EXISTS (SELECT 1 FROM viewer(@profile) v WHERE sees(v, items))
		ORDER BY id = @seed DESC, random()
		LIMIT @limit`,
		pgx.NamedArgs{"seed": seed, "genres": genres, "profile": profile, "limit": mixShown})
	if err != nil {
		return nil, err
	}
	return s.cards(ctx, profile, rows)
}
