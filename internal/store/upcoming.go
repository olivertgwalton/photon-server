package store

import (
	"context"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
)

// saveAiring replaces what a source says of a show's next episode to air.
func saveAiring(ctx context.Context, tx db, item uuid.UUID, source domain.FieldSource, a *domain.Airing) error {
	if a == nil {
		_, err := tx.Exec(ctx, `DELETE FROM next_airings WHERE item_id = $1 AND source = $2`, item, source)
		return err
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO next_airings (item_id, source, season_number, episode_number, title, air_date)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (item_id, source) DO UPDATE SET season_number = excluded.season_number,
			episode_number = excluded.episode_number, title = excluded.title, air_date = excluded.air_date`,
		item, source, a.SeasonNumber, a.EpisodeNumber, a.Title, a.Date)
	return err
}

// Upcoming is a show and its next episode to air.
type Upcoming struct {
	Show   Card
	Airing domain.Airing
}

// UpcomingQuery asks for a page of the shows a profile sees whose next episode airs today or later,
// in one library or all.
type UpcomingQuery struct {
	Profile uuid.UUID
	Library uuid.UUID
	Offset  int
	Limit   int
}

// Upcoming answers a page of the shows a profile sees with an episode due to air today or later,
// the soonest first, each with its next episode as the source its library ranks highest says; and
// how many there are in all.
func (s *Store) Upcoming(ctx context.Context, q UpcomingQuery) ([]Upcoming, int64, error) {
	var library *uuid.UUID
	if q.Library != (uuid.UUID{}) {
		library = &q.Library
	}
	args := pgx.NamedArgs{"profile": q.Profile, "library": library, "offset": q.Offset, "limit": q.Limit}
	const due = `
		FROM (
			SELECT DISTINCT ON (a.item_id) a.*
			FROM next_airings a
			JOIN items i ON i.id = a.item_id
			JOIN library_sources ls ON ls.library_id = i.library_id AND ls.source = a.source
				AND ls.item_kind = 'show' AND ls.fetcher = 'metadata' AND ls.enabled
			ORDER BY a.item_id, ls.position
		) a
		JOIN items i ON i.id = a.item_id
		WHERE a.air_date >= current_date
			AND (CAST(@library AS uuid) IS NULL OR i.library_id = CAST(@library AS uuid))
			AND EXISTS (SELECT 1 FROM viewer(CAST(@profile AS uuid)) v WHERE sees(v, i) AND first_of_title(v, i))`
	var total int64
	if err := s.pool.QueryRow(ctx, `SELECT count(*) `+due, args).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT a.item_id, a.season_number, a.episode_number, a.title, a.air_date `+due+`
		ORDER BY a.air_date, i.sort_title, i.id OFFSET @offset LIMIT @limit`, args)
	if err != nil {
		return nil, 0, err
	}
	var order []uuid.UUID
	airings := map[uuid.UUID]domain.Airing{}
	var id uuid.UUID
	var a domain.Airing
	if _, err := pgx.ForEachRow(rows, []any{&id, &a.SeasonNumber, &a.EpisodeNumber, &a.Title, &a.Date}, func() error {
		order = append(order, id)
		airings[id] = a
		return nil
	}); err != nil {
		return nil, 0, err
	}
	items, err := queryRows[model.Item](ctx, s.pool, `SELECT `+itemColumns+` FROM items WHERE id = ANY($1)`, order)
	if err != nil {
		return nil, 0, err
	}
	cards, err := s.cards(ctx, q.Profile, items)
	if err != nil {
		return nil, 0, err
	}
	byID := map[uuid.UUID]Card{}
	for _, c := range cards {
		byID[c.ID] = c
	}
	out := make([]Upcoming, len(order))
	for n, id := range order {
		out[n] = Upcoming{Show: byID[id], Airing: airings[id]}
	}
	return out, total, nil
}
