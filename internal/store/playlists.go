package store

import (
	"context"
	"slices"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
)

// PlaylistSummary is a playlist as its list shows it: how many entries it has and how long they run.
type PlaylistSummary struct {
	ID         uuid.UUID
	Name       string
	Entries    int
	DurationMS int64
	UpdatedAt  time.Time
}

// PlaylistEntry is one entry of a playlist: its own id, and the title it plays.
type PlaylistEntry struct {
	ID   uuid.UUID
	Card Card
}

// Playlists answers a profile's playlists, by name.
func (s *Store) Playlists(ctx context.Context, profile uuid.UUID) ([]PlaylistSummary, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT p.id, p.name, p.updated_at, count(e.id) AS entries,
			coalesce(sum((SELECT max(v.duration_ms) FROM versions v WHERE v.item_id = e.item_id AND v.missing_since IS NULL)), 0) AS duration_ms
		FROM playlists p LEFT JOIN playlist_entries e ON e.playlist_id = p.id
		WHERE p.profile_id = $1 GROUP BY p.id ORDER BY lower(p.name), p.id`, profile)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByName[PlaylistSummary])
}

// AddPlaylist makes a profile's playlist of titles (see AddToPlaylist).
func (s *Store) AddPlaylist(ctx context.Context, profile uuid.UUID, name string, items []uuid.UUID) (uuid.UUID, error) {
	var id uuid.UUID
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `INSERT INTO playlists (profile_id, name) VALUES ($1, $2) RETURNING id`, profile, name).Scan(&id)
		if err != nil {
			return err
		}
		return appendEntries(ctx, tx, id, items)
	})
	return id, err
}

// PlaylistEntries answers a page of a profile's playlist, in its order, and how many entries it
// has. ErrNotFound for no such playlist of the profile's.
func (s *Store) PlaylistEntries(ctx context.Context, profile, playlist uuid.UUID, offset, limit int) ([]PlaylistEntry, int64, error) {
	if err := ownPlaylist(ctx, s.pool, profile, playlist); err != nil {
		return nil, 0, err
	}
	// An entry the profile may no longer see, as a library taken from it, is passed over.
	const seen = `FROM playlist_entries e WHERE e.playlist_id = $1
		AND EXISTS (SELECT 1 FROM items i, viewer($2) v WHERE i.id = e.item_id AND sees(v, i))`
	var total int64
	if err := s.pool.QueryRow(ctx, `SELECT count(*) `+seen, playlist, profile).Scan(&total); err != nil {
		return nil, 0, err
	}
	entries, err := queryRows[model.PlaylistEntry](ctx, s.pool, `
		SELECT e.id, e.playlist_id, e.item_id, e.position `+seen+` ORDER BY e.position, e.id OFFSET $3 LIMIT $4`,
		playlist, profile, offset, limit)
	if err != nil {
		return nil, 0, err
	}
	in := make([]uuid.UUID, len(entries))
	for n, x := range entries {
		in[n] = x.ItemID
	}
	rows, err := queryRows[model.Item](ctx, s.pool, `SELECT `+itemColumns+` FROM items WHERE id = ANY($1)`, in)
	if err != nil {
		return nil, 0, err
	}
	cards, err := s.cards(ctx, profile, rows)
	if err != nil {
		return nil, 0, err
	}
	byID := map[uuid.UUID]Card{}
	for _, c := range cards {
		byID[c.ID] = c
	}
	out := make([]PlaylistEntry, len(entries))
	for n, x := range entries {
		out[n] = PlaylistEntry{ID: x.ID, Card: byID[x.ItemID]}
	}
	return out, total, nil
}

// AddToPlaylist puts titles at the end of a profile's playlist, in order: a film or episode as it
// is, a show or season as its episodes, a collection as its titles.
func (s *Store) AddToPlaylist(ctx context.Context, profile, playlist uuid.UUID, items []uuid.UUID) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := ownPlaylist(ctx, tx, profile, playlist); err != nil {
			return err
		}
		return appendEntries(ctx, tx, playlist, items)
	})
}

// RemoveFromPlaylist takes one entry out of a profile's playlist.
func (s *Store) RemoveFromPlaylist(ctx context.Context, profile, playlist, entry uuid.UUID) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := ownPlaylist(ctx, tx, profile, playlist); err != nil {
			return err
		}
		res, err := tx.Exec(ctx, `DELETE FROM playlist_entries WHERE playlist_id = $1 AND id = $2`, playlist, entry)
		if err == nil && res.RowsAffected() == 0 {
			err = ErrNotFound
		}
		if err != nil {
			return err
		}
		return touch(ctx, tx, playlist)
	})
}

// MovePlaylistEntry moves one entry of a profile's playlist to a position, counted from zero; one
// past the end goes last.
func (s *Store) MovePlaylistEntry(ctx context.Context, profile, playlist, entry uuid.UUID, position int) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := ownPlaylist(ctx, tx, profile, playlist); err != nil {
			return err
		}
		entries, err := queryRows[model.PlaylistEntry](ctx, tx, `
			SELECT id, playlist_id, item_id, position FROM playlist_entries WHERE playlist_id = $1 ORDER BY position, id`,
			playlist)
		if err != nil {
			return err
		}
		from := slices.IndexFunc(entries, func(x *model.PlaylistEntry) bool { return x.ID == entry })
		if from < 0 {
			return ErrNotFound
		}
		moved := entries[from]
		entries = slices.Delete(entries, from, from+1)
		entries = slices.Insert(entries, min(max(position, 0), len(entries)), moved)
		for n, x := range entries {
			if x.Position == n {
				continue
			}
			if _, err := tx.Exec(ctx, `UPDATE playlist_entries SET position = $2 WHERE id = $1`, x.ID, n); err != nil {
				return err
			}
		}
		return touch(ctx, tx, playlist)
	})
}

// RenamePlaylist renames a profile's playlist.
func (s *Store) RenamePlaylist(ctx context.Context, profile, playlist uuid.UUID, name string) error {
	res, err := s.pool.Exec(ctx, `UPDATE playlists SET name = $3 WHERE id = $1 AND profile_id = $2`, playlist, profile, name)
	if err == nil && res.RowsAffected() == 0 {
		err = ErrNotFound
	}
	return err
}

// RemovePlaylist removes a profile's playlist.
func (s *Store) RemovePlaylist(ctx context.Context, profile, playlist uuid.UUID) error {
	res, err := s.pool.Exec(ctx, `DELETE FROM playlists WHERE id = $1 AND profile_id = $2`, playlist, profile)
	if err == nil && res.RowsAffected() == 0 {
		err = ErrNotFound
	}
	return err
}

func ownPlaylist(ctx context.Context, q db, profile, playlist uuid.UUID) error {
	var one int
	return found(q.QueryRow(ctx, `SELECT 1 FROM playlists WHERE id = $1 AND profile_id = $2`, playlist, profile).Scan(&one))
}

func touch(ctx context.Context, tx db, playlist uuid.UUID) error {
	_, err := tx.Exec(ctx, `UPDATE playlists SET updated_at = $2 WHERE id = $1`, playlist, time.Now())
	return err
}

// appendEntries puts the films and episodes titles stand for after a playlist's last entry.
func appendEntries(ctx context.Context, tx db, playlist uuid.UUID, items []uuid.UUID) error {
	var playable []uuid.UUID
	for _, id := range items {
		more, err := playableOf(ctx, tx, id)
		if err != nil {
			return err
		}
		playable = append(playable, more...)
	}
	if len(playable) > 0 {
		_, err := tx.Exec(ctx, `
			INSERT INTO playlist_entries (playlist_id, item_id, position)
			SELECT $1, item, coalesce((SELECT max(position) + 1 FROM playlist_entries WHERE playlist_id = $1), 0) + n - 1
			FROM unnest($2::uuid[]) WITH ORDINALITY AS t(item, n)`, playlist, playable)
		if err != nil {
			return err
		}
	}
	return touch(ctx, tx, playlist)
}

// playableOf answers the films and episodes a title stands for, in the order they play; ErrNotFound
// for no title, or one that plays nothing, such as an extra.
func playableOf(ctx context.Context, tx db, id uuid.UUID) ([]uuid.UUID, error) {
	var kind domain.ItemKind
	if err := tx.QueryRow(ctx, `SELECT kind FROM items WHERE id = $1`, id).Scan(&kind); err != nil {
		return nil, found(err)
	}
	var sql string
	switch kind {
	case domain.ItemMovie, domain.ItemEpisode:
		return []uuid.UUID{id}, nil
	case domain.ItemSeason:
		sql = `SELECT id FROM items WHERE parent_id = $1 AND kind = 'episode' ORDER BY episode_number, id`
	case domain.ItemShow:
		sql = `SELECT e.id FROM items e JOIN items s ON s.id = e.parent_id
			WHERE s.parent_id = $1 AND e.kind = 'episode'
			ORDER BY s.season_number = 0, s.season_number, e.episode_number, e.id`
	case domain.ItemCollection:
		sql = `SELECT m.item_id FROM collection_members m JOIN collections c ON c.item_id = m.collection_id
			JOIN items i ON i.id = m.item_id WHERE m.collection_id = $1
			ORDER BY CASE WHEN c.origin = 'user' THEN m.position END, i.released_asc, i.id`
	case domain.ItemExtra:
		return nil, ErrNotFound
	}
	rows, err := tx.Query(ctx, sql, id)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil || kind != domain.ItemCollection {
		return out, err
	}
	var all []uuid.UUID
	for _, m := range out {
		more, err := playableOf(ctx, tx, m)
		if err != nil {
			return nil, err
		}
		all = append(all, more...)
	}
	return all, nil
}
