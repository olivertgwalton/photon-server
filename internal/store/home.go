package store

import (
	"cmp"
	"context"
	"errors"
	"uuid"

	"gorm.io/gorm"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
)

// HomeRow is one row of a profile's home page.
type HomeRow struct {
	Kind  domain.HomeRow
	Cards []Card
}

// rowQueries are the rows' queries, each taking the profile and a limit, and holding only what the
// profile may see. gen cannot write a
// lateral join or a window, so they are SQL.
var rowQueries = map[domain.HomeRow]string{
	domain.RowContinueWatching: `
		SELECT i.* FROM watch_state w JOIN items i ON i.id = w.item_id
		WHERE w.profile_id = @profile AND w.position_ms > 0 AND i.kind IN ('movie', 'episode') AND visible(i.id, @profile)
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
			WHERE w.profile_id = @profile AND w.watched_at IS NOT NULL AND e.season_number > 0 AND visible(show.id, @profile)
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
		WHERE f.profile_id = @profile AND visible(i.id, @profile) ORDER BY f.added_at DESC LIMIT @limit`,
	domain.RowRecentFilms: `
		SELECT * FROM items WHERE kind = 'movie' AND visible(id, @profile)
		ORDER BY added_at DESC, id DESC LIMIT @limit`,
	domain.RowRecentShows: `
		SELECT show.* FROM items show
		JOIN items season ON season.parent_id = show.id AND season.kind = 'season'
		JOIN items e ON e.parent_id = season.id AND e.kind = 'episode'
		WHERE show.kind = 'show' AND visible(show.id, @profile)
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

// ErrNoNext is a title with no episode to play next: a film, a show with no episodes the profile
// has, or the last episode.
var ErrNoNext = errors.New("no episode follows")

// nextQueries answer the episode to play next, in the show's order (its seasons and episodes as
// its files number them, in whichever order it is set to) and, as the next up row, specials aside
// unless the title is one of them. A show's episodes are visible to a profile as the show is.
var nextQueries = map[domain.ItemKind]string{
	// After an episode, the one after it, watched or not, as a player's Up Next.
	domain.ItemEpisode: `
		SELECT e.* FROM items e
		JOIN items season ON season.id = e.parent_id
		JOIN items here ON here.id = @id
		WHERE e.kind = 'episode' AND season.parent_id = (SELECT parent_id FROM items WHERE id = here.parent_id)
			AND (e.season_number, coalesce(e.episode_number, 0)) > (here.season_number, coalesce(here.episode_number, 0))
			AND (e.season_number > 0 OR here.season_number = 0)
		ORDER BY e.season_number, e.episode_number, e.id LIMIT 1`,
	// Of a show or a season, where the profile is: the episode under way, else the first unwatched
	// after the last one watched, else the first.
	domain.ItemShow:   resumeQuery,
	domain.ItemSeason: resumeQuery,
}

const resumeQuery = `
	WITH e AS (
		SELECT e.id, coalesce(w.position_ms, 0) > 0 AS started, w.watched_at IS NOT NULL AS watched,
			w.last_played_at, row_number() OVER (ORDER BY e.season_number, e.episode_number, e.id) AS n
		FROM items e
		JOIN items season ON season.id = e.parent_id
		LEFT JOIN watch_state w ON w.item_id = e.id AND w.profile_id = @profile
		WHERE e.kind = 'episode' AND (season.id = @id OR (season.parent_id = @id AND season.season_number > 0))
	), last AS (
		SELECT n FROM e WHERE watched ORDER BY last_played_at DESC NULLS LAST, n DESC LIMIT 1
	)
	SELECT * FROM items WHERE id = (
		SELECT id FROM e ORDER BY
			CASE WHEN started THEN 0 WHEN NOT watched AND n > coalesce((SELECT n FROM last), 0) THEN 1 ELSE 2 END,
			CASE WHEN started THEN last_played_at END DESC NULLS LAST, n
		LIMIT 1)`

// Next answers the episode to play after a title, as a card: ErrNotFound for a title the profile
// may not see, ErrNoNext where nothing follows.
func (s *Store) Next(ctx context.Context, profile, id uuid.UUID) (Card, error) {
	i := s.q.Item
	item, err := i.WithContext(ctx).Where(i.ID.Eq(model.UUID(id))).Take()
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Card{}, ErrNotFound
	}
	if err != nil {
		return Card{}, err
	}
	if ok, err := s.visible(ctx, profile, id); err != nil || !ok {
		return Card{}, cmp.Or(err, ErrNotFound)
	}
	q, ok := nextQueries[item.Kind]
	if !ok {
		return Card{}, ErrNoNext
	}
	var rows []*model.Item
	err = i.WithContext(ctx).UnderlyingDB().Raw(q, map[string]any{"profile": model.UUID(profile), "id": model.UUID(id)}).Find(&rows).Error
	if err != nil {
		return Card{}, err
	}
	if len(rows) == 0 {
		return Card{}, ErrNoNext
	}
	cards, err := s.cards(ctx, profile, rows)
	if err != nil {
		return Card{}, err
	}
	return cards[0], nil
}
