package store

import (
	"context"
	"errors"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
)

// TitleState is what a profile has made of a title. A show's and a season's are their episodes':
// watched once every one is, with how many are left.
type TitleState struct {
	PositionMS   int64
	Plays        int
	WatchedAt    *time.Time
	LastPlayedAt *time.Time
	FavouriteAt  *time.Time
	// WatchlistedAt is when a film or show went on the watchlist.
	WatchlistedAt *time.Time
	Unwatched     int
}

// ErrNotListable is a title that cannot go on the watchlist.
var ErrNotListable = errors.New("only a film or a show, or a show's season or episode, goes on the watchlist")

// ErrSuperseded is progress from before the profile's state of the title last changed.
var ErrSuperseded = errors.New("the title's state has changed since then")

// newest keeps a write only if it happened no earlier than the state it replaces last changed.
// Each write's time is at most the database's now, so a client clock running ahead holds off
// nothing written after it.
const newest = ` WHERE watch_state.changed_at IS NULL OR watch_state.changed_at <= excluded.changed_at`

// Length is how long a film or episode runs, as its progress is measured: its longest copy's.
func (s *Store) Length(ctx context.Context, item uuid.UUID) (time.Duration, error) {
	lengths, err := s.durations(ctx, []uuid.UUID{item})
	return time.Duration(lengths[item].ms) * time.Millisecond, err
}

// SaveProgress records that a profile stopped a film or episode, running length, at position, at a
// time or now for none, and answers how far that got: too near the start to keep, somewhere to
// resume, or far enough to count as watched. ErrSuperseded if the state has changed since.
// before is how far the viewing had already got: the play is counted as it first reaches the end,
// once however often a player reports from there, as Jellyfin counts a play and Plex scrobbles.
func (s *Store) SaveProgress(ctx context.Context, profile, item uuid.UUID, position, length time.Duration, before domain.Reach, at *time.Time) (domain.Reach, error) {
	reach := domain.ReachOf(position, length)
	var tag pgconn.CommandTag
	var err error
	switch reach {
	case domain.ReachEnd:
		if before == domain.ReachEnd {
			tag, err = s.watched(ctx, profile, []uuid.UUID{item}, at)
		} else {
			tag, err = s.played(ctx, profile, item, at)
		}
	case domain.ReachStart, domain.ReachResumable:
		// A peek at the start is not a play, as Jellyfin's is not: it moves nothing up Next Up.
		if reach == domain.ReachStart {
			position = 0
		}
		tag, err = s.pool.Exec(ctx, `
			INSERT INTO watch_state (profile_id, item_id, position_ms, last_played_at, changed_at)
			SELECT $1, t, $3::bigint, CASE WHEN $3::bigint > 0 THEN w END, w
			FROM same_title($2) t, least($4::timestamptz, now()) w ORDER BY t
			ON CONFLICT (profile_id, item_id) DO UPDATE SET position_ms = excluded.position_ms,
				last_played_at = coalesce(excluded.last_played_at, watch_state.last_played_at),
				changed_at = excluded.changed_at`+newest,
			profile, item, position.Milliseconds(), at)
	}
	switch {
	case violates(err, foreignKeyViolation):
		// A title removed is written all the same, as same_title answers it, and refused.
		return reach, ErrNotFound
	case err == nil && tag.RowsAffected() == 0:
		return reach, ErrSuperseded
	}
	return reach, err
}

// MarkWatched marks a film or episode watched, or every episode of a season or show, at a time or
// now for none; one whose state has changed since is left as it is.
func (s *Store) MarkWatched(ctx context.Context, profile, item uuid.UUID, at *time.Time) error {
	leaves, err := s.leaves(ctx, item)
	if err != nil {
		return err
	}
	_, err = s.watched(ctx, profile, ids(leaves), at)
	return err
}

// watched marks titles watched without counting a play: one marked by hand has been played at
// least once, and keeps when it was first watched, as Jellyfin's MarkPlayed does. A profile's state
// of a title is its state wherever the title is listed, so each of these writes them all.
func (s *Store) watched(ctx context.Context, profile uuid.UUID, items []uuid.UUID, at *time.Time) (pgconn.CommandTag, error) {
	if len(items) == 0 {
		return pgconn.CommandTag{}, nil
	}
	return s.finish(ctx, profile, items, `
		INSERT INTO watch_state (profile_id, item_id, plays, watched_at, last_played_at, changed_at)
		SELECT DISTINCT $1::uuid, t, 1, w, w, w
		FROM unnest($2::uuid[]) i, same_title(i) t, least($3::timestamptz, now()) w ORDER BY t
		ON CONFLICT (profile_id, item_id) DO UPDATE SET
			position_ms = 0, plays = greatest(watch_state.plays, 1),
			watched_at = coalesce(watch_state.watched_at, excluded.watched_at),
			last_played_at = excluded.last_played_at, changed_at = excluded.changed_at`+newest,
		profile, items, at)
}

// played counts a viewing that reached the end.
func (s *Store) played(ctx context.Context, profile, item uuid.UUID, at *time.Time) (pgconn.CommandTag, error) {
	return s.finish(ctx, profile, []uuid.UUID{item}, `
		INSERT INTO watch_state (profile_id, item_id, plays, watched_at, last_played_at, changed_at)
		SELECT $1, t, 1, w, w, w FROM same_title($2) t, least($3::timestamptz, now()) w ORDER BY t
		ON CONFLICT (profile_id, item_id) DO UPDATE SET
			position_ms = 0, plays = watch_state.plays + 1,
			watched_at = coalesce(watch_state.watched_at, excluded.watched_at),
			last_played_at = excluded.last_played_at, changed_at = excluded.changed_at`+newest,
		profile, item, at)
}

// finish runs write, which marks films or episodes watched, and takes from the watchlist each film
// it marked and each show it left watched through, as Plex takes a title watched off it. Nothing is
// taken off for a write superseded.
func (s *Store) finish(ctx context.Context, profile uuid.UUID, items []uuid.UUID, write string, args ...any) (pgconn.CommandTag, error) {
	var tag pgconn.CommandTag
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		if tag, err = tx.Exec(ctx, write, args...); err != nil || tag.RowsAffected() == 0 {
			return err
		}
		_, err = tx.Exec(ctx, `
			DELETE FROM watchlist USING items
			WHERE watchlist.profile_id = @profile AND items.id = watchlist.item_id AND items.id IN (
				SELECT same_title(coalesce(season.parent_id, i.id)) FROM items i
				LEFT JOIN items season ON season.id = i.parent_id AND i.kind = 'episode'
				WHERE i.id = ANY(@items))
			AND `+markSQL(domain.MarkWatched), pgx.NamedArgs{"profile": profile, "items": items})
		return err
	})
	return tag, err
}

// MarkUnwatched forgets that a film or episode, or every episode of a season or show, was
// watched, and where it stopped.
func (s *Store) MarkUnwatched(ctx context.Context, profile, item uuid.UUID) error {
	return s.setLeaves(ctx, profile, item, `watched_at = NULL, position_ms = 0, changed_at = now()`)
}

// ClearProgress forgets where a film or episode, or each episode of a season or show, stopped,
// taking it out of Continue Watching; whether it was watched, and its plays, stay.
func (s *Store) ClearProgress(ctx context.Context, profile, item uuid.UUID) error {
	return s.setLeaves(ctx, profile, item, `position_ms = 0, changed_at = now()`)
}

// setLeaves writes set to a profile's state of a title's leaves, wherever each is listed.
func (s *Store) setLeaves(ctx context.Context, profile, item uuid.UUID, set string) error {
	leaves, err := s.leaves(ctx, item)
	if err != nil || len(leaves) == 0 {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		UPDATE watch_state SET `+set+`
		WHERE profile_id = $1 AND item_id IN (SELECT same_title(i) FROM unnest($2::uuid[]) i)`, profile, ids(leaves))
	return err
}

func (s *Store) Favourite(ctx context.Context, profile, item uuid.UUID) error {
	if _, err := s.leaves(ctx, item); err != nil {
		return err
	}
	return s.list(ctx, "favourites", profile, item)
}

func (s *Store) Unfavourite(ctx context.Context, profile, item uuid.UUID) error {
	return s.unlist(ctx, "favourites", profile, item)
}

// Watchlist puts a film or show on the profile's watchlist: a season or episode, its show.
// ErrNotListable for anything else.
func (s *Store) Watchlist(ctx context.Context, profile, item uuid.UUID) error {
	title, err := s.listable(ctx, item)
	if err != nil {
		return err
	}
	return s.list(ctx, "watchlist", profile, title)
}

// Unwatchlist takes a film or show from the profile's watchlist: a season or episode, its show.
func (s *Store) Unwatchlist(ctx context.Context, profile, item uuid.UUID) error {
	title, err := s.listable(ctx, item)
	if err != nil {
		return err
	}
	return s.unlist(ctx, "watchlist", profile, title)
}

// listable answers the title that goes on the watchlist for a title, as Plex lists only films and
// shows.
func (s *Store) listable(ctx context.Context, id uuid.UUID) (uuid.UUID, error) {
	row, err := readItem(ctx, s.pool, id)
	if err != nil {
		return uuid.UUID{}, err
	}
	switch row.Kind {
	case domain.ItemMovie, domain.ItemShow:
		return row.ID, nil
	case domain.ItemSeason, domain.ItemEpisode:
		return s.listable(ctx, *row.ParentID)
	case domain.ItemExtra, domain.ItemCollection:
	}
	return uuid.UUID{}, ErrNotListable
}

// list puts a title, wherever it is listed, on one of the profile's lists: favourites or watchlist.
func (s *Store) list(ctx context.Context, table string, profile, item uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO `+table+` (profile_id, item_id) SELECT $1, t FROM same_title($2) t ORDER BY t
		ON CONFLICT DO NOTHING`, profile, item)
	return err
}

func (s *Store) unlist(ctx context.Context, table string, profile, item uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM `+table+` WHERE profile_id = $1 AND item_id IN (SELECT same_title($2))`,
		profile, item)
	return err
}

// keyTitle keys a title, its seasons and episodes by what they are wherever they are listed, and
// makes each profile's state of them its state of the titles they are the same as.
func keyTitle(ctx context.Context, tx db, title uuid.UUID) error {
	_, err := tx.Exec(ctx, `SELECT key_titles(ARRAY[$1::uuid])`, title)
	return err
}

// leaves answers the films or episodes a title is watched by: itself, or a season's or show's
// episodes. ErrNotFound for no title.
func (s *Store) leaves(ctx context.Context, id uuid.UUID) ([]*model.Item, error) {
	row, err := readItem(ctx, s.pool, id)
	if err != nil {
		return nil, err
	}
	parents := []*model.Item{row}
	switch row.Kind {
	case domain.ItemMovie, domain.ItemEpisode, domain.ItemExtra:
		return parents, nil
	case domain.ItemShow:
		parents, err = queryRows[model.Item](ctx, s.pool, `SELECT `+itemColumns+` FROM items WHERE parent_id = $1 AND kind = 'season'`, row.ID)
		if err != nil {
			return nil, err
		}
	case domain.ItemSeason:
	case domain.ItemCollection:
		// A box set marked is each of its titles marked, as Jellyfin's is.
		return queryRows[model.Item](ctx, s.pool, `
			SELECT `+itemColumns+` FROM items
			WHERE (kind = 'movie' AND id IN (SELECT item_id FROM collection_members WHERE collection_id = $1))
				OR (kind = 'episode' AND parent_id IN (SELECT s.id FROM items s
					JOIN collection_members m ON s.parent_id = m.item_id WHERE m.collection_id = $1))`, row.ID)
	}
	return queryRows[model.Item](ctx, s.pool, `SELECT `+itemColumns+` FROM items WHERE parent_id = ANY($1) AND kind = 'episode'`, ids(parents))
}

type episodeCount struct {
	ID         uuid.UUID
	Episodes   int
	Watched    int
	LastPlayed *time.Time
	WatchedAt  *time.Time
}

// states answers what a profile has made of each title.
func (s *Store) states(ctx context.Context, profile uuid.UUID, items []*model.Item) (map[uuid.UUID]TitleState, error) {
	out := map[uuid.UUID]TitleState{}
	if len(items) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT item_id, position_ms, plays, watched_at, last_played_at FROM watch_state
		WHERE profile_id = $1 AND item_id = ANY($2)`, profile, ids(items))
	if err != nil {
		return nil, err
	}
	var item uuid.UUID
	var st TitleState
	_, err = pgx.ForEachRow(rows, []any{&item, &st.PositionMS, &st.Plays, &st.WatchedAt, &st.LastPlayedAt}, func() error {
		out[item] = st
		return nil
	})
	if err != nil {
		return nil, err
	}
	var groups []*model.Item
	for _, it := range items {
		if it.Kind == domain.ItemShow || it.Kind == domain.ItemSeason {
			groups = append(groups, it)
		}
	}
	if len(groups) > 0 {
		counts, err := queryStructs[episodeCount](ctx, s.pool, `
			SELECT g.id, count(e.id) AS episodes, count(ws.watched_at) AS watched,
				max(ws.last_played_at) AS last_played, max(ws.watched_at) AS watched_at
			FROM items g
			CROSS JOIN LATERAL (
				SELECT e.id FROM items e WHERE e.parent_id = g.id AND e.kind = 'episode'
				UNION ALL
				SELECT e.id FROM items s JOIN items e ON e.parent_id = s.id WHERE s.parent_id = g.id AND e.kind = 'episode'
			) e
			LEFT JOIN watch_state ws ON ws.item_id = e.id AND ws.profile_id = $1
			WHERE g.id = ANY($2::uuid[])
			GROUP BY g.id`, profile, ids(groups))
		if err != nil {
			return nil, err
		}
		for _, c := range counts {
			st := TitleState{Unwatched: c.Episodes - c.Watched, LastPlayedAt: c.LastPlayed}
			if st.Unwatched == 0 {
				st.WatchedAt = c.WatchedAt
			}
			out[c.ID] = st
		}
	}
	rows, err = s.pool.Query(ctx, `
		SELECT item_id, f.added_at, l.added_at
		FROM (SELECT item_id, added_at FROM favourites WHERE profile_id = $1 AND item_id = ANY($2)) f
		FULL JOIN (SELECT item_id, added_at FROM watchlist WHERE profile_id = $1 AND item_id = ANY($2)) l USING (item_id)`,
		profile, ids(items))
	if err != nil {
		return nil, err
	}
	var favourite, listed *time.Time
	_, err = pgx.ForEachRow(rows, []any{&item, &favourite, &listed}, func() error {
		st := out[item]
		st.FavouriteAt, st.WatchlistedAt = favourite, listed
		out[item] = st
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// RecordPlay keeps a playback in the history as it stops.
func (s *Store) RecordPlay(ctx context.Context, p domain.Playback, stopped time.Time, position time.Duration) error {
	var version *uuid.UUID
	if p.Version != (uuid.UUID{}) {
		version = &p.Version
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO plays (profile_id, item_id, version_id, method, started_at, stopped_at, position_ms)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		p.Profile, p.Item, version, p.Method, p.Started, stopped, position.Milliseconds())
	// A title removed while it played leaves nothing to keep.
	if violates(err, foreignKeyViolation) {
		return nil
	}
	return err
}

// Play is one playback in the history: who, of what, how, when and how far.
type Play struct {
	ID         uuid.UUID
	Profile    uuid.UUID
	Card       Card
	Method     domain.PlayMethod
	StartedAt  time.Time
	StoppedAt  time.Time
	PositionMS int64
}

// History answers a page of plays, the latest first, and how many there are: a profile's, or
// everyone's for none.
func (s *Store) History(ctx context.Context, profile uuid.UUID, offset, limit int) ([]Play, int64, error) {
	var who *uuid.UUID
	if profile != (uuid.UUID{}) {
		who = &profile
	}
	const whose = ` FROM plays WHERE $1::uuid IS NULL OR profile_id = $1`
	var total int64
	if err := s.pool.QueryRow(ctx, `SELECT count(*)`+whose, who).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := queryRows[model.Play](ctx, s.pool, `
		SELECT id, profile_id, item_id, method, started_at, stopped_at, position_ms`+whose+`
		ORDER BY stopped_at DESC, id DESC OFFSET $2 LIMIT $3`, who, offset, limit)
	if err != nil || len(rows) == 0 {
		return []Play{}, total, err
	}
	in := make([]uuid.UUID, len(rows))
	for n, r := range rows {
		in[n] = r.ItemID
	}
	items, err := queryRows[model.Item](ctx, s.pool, `SELECT `+itemColumns+` FROM items WHERE id = ANY($1)`, in)
	if err != nil {
		return nil, 0, err
	}
	cards, err := s.cards(ctx, profile, items)
	if err != nil {
		return nil, 0, err
	}
	byID := map[uuid.UUID]Card{}
	for _, c := range cards {
		byID[c.ID] = c
	}
	out := make([]Play, len(rows))
	for n, r := range rows {
		out[n] = Play{
			ID: r.ID, Profile: r.ProfileID, Card: byID[r.ItemID], Method: r.Method,
			StartedAt: r.StartedAt, StoppedAt: r.StoppedAt, PositionMS: r.PositionMS,
		}
	}
	return out, total, nil
}
