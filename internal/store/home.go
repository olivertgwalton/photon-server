package store

import (
	"context"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
)

// HomeRow is one row of a profile's home page.
type HomeRow struct {
	Kind  domain.HomeRow
	Cards []Card
}

// rowQueries are the rows' queries, each taking the profile and a limit. gen cannot write a
// lateral join or a window, so they are SQL.
var rowQueries = map[domain.HomeRow]string{
	domain.RowContinueWatching: `
		SELECT i.* FROM watch_state w JOIN items i ON i.id = w.item_id
		WHERE w.profile_id = @profile AND w.position_ms > 0 AND i.kind IN ('movie', 'episode')
		ORDER BY w.last_played_at DESC LIMIT @limit`,
	// As Jellyfin's: the episode after the last one watched of each show, in the show's order and
	// specials aside, unless it is under way already and so in Continue Watching.
	domain.RowNextUp: `
		WITH last AS (
			SELECT DISTINCT ON (show.id) show.id AS show_id, e.season_number, e.episode_number,
				w.last_played_at
			FROM watch_state w
			JOIN items e ON e.id = w.item_id AND e.kind = 'episode'
			JOIN items season ON season.id = e.parent_id
			JOIN items show ON show.id = season.parent_id
			WHERE w.profile_id = @profile AND w.watched_at IS NOT NULL AND e.season_number > 0
			ORDER BY show.id, w.last_played_at DESC
		)
		SELECT next.* FROM last
		CROSS JOIN LATERAL (
			SELECT e.* FROM items season
			JOIN items e ON e.parent_id = season.id AND e.kind = 'episode'
			LEFT JOIN watch_state w ON w.item_id = e.id AND w.profile_id = @profile
			WHERE season.parent_id = last.show_id AND season.kind = 'season' AND season.season_number > 0
				AND (e.season_number, coalesce(e.episode_number, 0)) > (last.season_number, coalesce(last.episode_number, 0))
				AND w.watched_at IS NULL
			ORDER BY e.season_number, e.episode_number LIMIT 1
		) next
		LEFT JOIN watch_state started ON started.item_id = next.id AND started.profile_id = @profile
		WHERE coalesce(started.position_ms, 0) = 0
		ORDER BY last.last_played_at DESC LIMIT @limit`,
	domain.RowFavourites: `
		SELECT i.* FROM favourites f JOIN items i ON i.id = f.item_id
		WHERE f.profile_id = @profile ORDER BY f.added_at DESC LIMIT @limit`,
	domain.RowRecentFilms: `
		SELECT * FROM items WHERE kind = 'movie'
		ORDER BY added_at DESC, id DESC LIMIT @limit`,
	domain.RowRecentShows: `
		SELECT show.* FROM items show
		JOIN items season ON season.parent_id = show.id AND season.kind = 'season'
		JOIN items e ON e.parent_id = season.id AND e.kind = 'episode'
		WHERE show.kind = 'show'
		GROUP BY show.id ORDER BY max(e.added_at) DESC, show.id DESC LIMIT @limit`,
}

// Home answers a profile's home page: each row with anything in it, up to limit cards each.
func (s *Store) Home(ctx context.Context, profile uuid.UUID, limit int) ([]HomeRow, error) {
	var rows []HomeRow
	db := s.q.Item.WithContext(ctx).UnderlyingDB()
	for _, kind := range domain.HomeRows() {
		var items []*model.Item
		err := db.Raw(rowQueries[kind], map[string]any{"profile": model.UUID(profile), "limit": limit}).Find(&items).Error
		if err != nil {
			return nil, err
		}
		if len(items) == 0 {
			continue
		}
		cards, err := s.cards(ctx, profile, items)
		if err != nil {
			return nil, err
		}
		rows = append(rows, HomeRow{Kind: kind, Cards: cards})
	}
	return rows, nil
}
