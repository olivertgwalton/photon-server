package store

import (
	"context"
	"slices"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
)

// shownPoster is the poster a collection's card wears, and its BlurHash.
type shownPoster struct {
	Collection uuid.UUID
	ID         uuid.UUID
	Blurhash   string
}

// collectionPosters answers a poster for each collection among rows that has none of its own: the
// first poster of its titles the profile sees, or a mosaic of the first four, as Plex draws a
// collection, in its titles' order; an episode gives its show's, as Jellyfin's box sets take it,
// and a poster is laid down once. A mosaic is named by its posters, so the same posters are the
// same picture whoever is shown it, and kept for the picture cache to lay out.
func (s *Store) collectionPosters(ctx context.Context, profile uuid.UUID, rows []*model.Item) ([]shownPoster, error) {
	if !slices.ContainsFunc(rows, func(r *model.Item) bool { return r.Kind == domain.ItemCollection }) {
		return nil, nil
	}
	return queryStructs[shownPoster](ctx, s.pool, `
		WITH pictured AS (
			SELECT m.collection_id, b.artwork_id, b.blurhash, row_number() OVER (PARTITION BY m.collection_id ORDER BY `+memberOrder+`) AS n
			FROM collection_members m JOIN collections c ON c.item_id = m.collection_id JOIN items ON items.id = m.item_id
			JOIN best_artwork b ON b.kind = 'poster' AND b.item_id = CASE WHEN items.kind = 'episode'
				THEN (SELECT s.parent_id FROM items s WHERE s.id = items.parent_id) ELSE items.id END
			WHERE m.collection_id = ANY($1) AND NOT EXISTS (SELECT 1 FROM best_artwork o WHERE o.item_id = m.collection_id AND o.kind = 'poster')
				AND EXISTS (SELECT 1 FROM viewer($2) v WHERE sees(v, items))
		), firsts AS (
			SELECT collection_id, artwork_id, min(blurhash) AS blurhash, min(n) AS n FROM pictured GROUP BY collection_id, artwork_id
		), chosen AS (
			SELECT collection_id, (array_agg(artwork_id ORDER BY n))[1:4] AS parts, (array_agg(blurhash ORDER BY n))[1] AS blurhash
			FROM firsts GROUP BY collection_id
		), shown AS (
			SELECT collection_id, parts, blurhash, CASE WHEN cardinality(parts) = 1 THEN parts[1] ELSE encode(substr(sha256('mosaic'::bytea
				|| (SELECT string_agg(uuid_send(p), ''::bytea ORDER BY o) FROM unnest(parts) WITH ORDINALITY u (p, o))), 1, 16), 'hex')::uuid END AS id
			FROM chosen
		), made AS (
			INSERT INTO mosaics (id, collection_id, parts) SELECT id, collection_id, parts FROM shown WHERE cardinality(parts) > 1
			ON CONFLICT DO NOTHING
		)
		SELECT shown.collection_id AS collection, shown.id,
			coalesce(CASE WHEN cardinality(shown.parts) = 1 THEN shown.blurhash ELSE m.blurhash END, '') AS blurhash
		FROM shown LEFT JOIN mosaics m ON m.id = shown.id`, ids(rows), profile)
}

// wearPosters puts each collection's shown poster among its pictures, and its BlurHash among hashes.
func wearPosters(pictures map[uuid.UUID]map[domain.ArtworkKind][]uuid.UUID, hashes map[uuid.UUID]string, posters []shownPoster) {
	for _, p := range posters {
		if pictures[p.Collection] == nil {
			pictures[p.Collection] = map[domain.ArtworkKind][]uuid.UUID{}
		}
		pictures[p.Collection][domain.ArtworkPoster] = []uuid.UUID{p.ID}
		if p.Blurhash != "" {
			hashes[p.ID] = p.Blurhash
		}
	}
}

// mosaic is the picture of a mosaic of these posters, each where it is.
func (s *Store) mosaic(ctx context.Context, parts []uuid.UUID) (domain.Picture, error) {
	tiles := make([]domain.Tile, len(parts))
	for n, part := range parts {
		p, err := s.Picture(ctx, part)
		if err != nil {
			return domain.Picture{}, err
		}
		tiles[n] = domain.Tile{ID: part, Picture: p}
	}
	return domain.Picture{Mosaic: tiles}, nil
}
