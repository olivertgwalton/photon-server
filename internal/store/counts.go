package store

import (
	"context"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// LibraryCounts counts each library's films, shows, seasons, episodes and listed collections, as
// the server holds them.
func (s *Store) LibraryCounts(ctx context.Context) (map[uuid.UUID]domain.TitleCounts, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT library_id, kind, count(*) FROM items WHERE kind IN ('movie', 'show', 'season', 'episode')
		GROUP BY 1, 2
		UNION ALL
		SELECT items.library_id, items.kind, count(*) FROM items, viewer(NULL) v
		WHERE `+listedCollection+`
		GROUP BY 1, 2`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID]domain.TitleCounts{}
	for rows.Next() {
		var id uuid.UUID
		var kind domain.ItemKind
		var n int
		if err := rows.Scan(&id, &kind, &n); err != nil {
			return nil, err
		}
		c := out[id]
		switch kind {
		case domain.ItemMovie:
			c.Movies = n
		case domain.ItemShow:
			c.Shows = n
		case domain.ItemSeason:
			c.Seasons = n
		case domain.ItemEpisode:
			c.Episodes = n
		case domain.ItemCollection:
			c.Collections = n
		case domain.ItemExtra:
		}
		out[id] = c
	}
	return out, rows.Err()
}
