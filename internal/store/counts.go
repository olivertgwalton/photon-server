package store

import (
	"context"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// LibraryCounts counts each library's films, shows, seasons, episodes, and the listed collections
// holding its titles, as the server holds them.
func (s *Store) LibraryCounts(ctx context.Context) (map[uuid.UUID]domain.TitleCounts, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT library_id, kind, count(*) FROM items WHERE kind IN ('movie', 'show', 'season', 'episode')
		GROUP BY 1, 2
		UNION ALL
		SELECT held.library_id, items.kind, count(*) FROM items JOIN collections c ON c.item_id = items.id, viewer(NULL) v,
			LATERAL (SELECT title.library_id FROM collection_members held JOIN items title ON title.id = held.item_id
				WHERE held.collection_id = items.id GROUP BY 1 HAVING count(*) >= `+listedFrom+`) held
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
