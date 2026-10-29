package store

import (
	"context"
	"errors"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// ErrNotMergeable refuses to merge a title that is not another film of the same library.
var ErrNotMergeable = errors.New("title_id is not another film of the same library")

// MergeTitle merges a film into another, as Plex's Merge does: its versions and extras become the
// other's, each profile's state of it is kept as one row of the two would have kept it, and it goes.
// A scan finds the film it was merged into by those versions and leaves them there, with that
// film's own name and description, as a split film stays apart.
func (s *Store) MergeTitle(ctx context.Context, id, other uuid.UUID) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		item, err := readItem(ctx, tx, id)
		if err != nil {
			return err
		}
		if item.Kind != domain.ItemMovie {
			return ErrNotFound
		}
		merged, err := readItem(ctx, tx, other)
		if errors.Is(err, ErrNotFound) {
			return ErrNotMergeable
		}
		if err != nil {
			return err
		}
		if other == id || merged.Kind != domain.ItemMovie || *merged.LibraryID != *item.LibraryID {
			return ErrNotMergeable
		}
		b := &pgx.Batch{}
		b.Queue(`UPDATE versions SET item_id = $1, merged_at = now(), split_at = NULL WHERE item_id = $2`, id, other)
		b.Queue(`UPDATE items SET parent_id = $1 WHERE parent_id = $2`, id, other)
		// Merged as group_titles merges the state of titles that are the same.
		b.Queue(`
			INSERT INTO watch_state (profile_id, item_id, position_ms, plays, watched_at, last_played_at)
			SELECT profile_id, $1, (array_agg(position_ms ORDER BY last_played_at DESC NULLS LAST))[1],
				max(plays), min(watched_at), max(last_played_at)
			FROM watch_state WHERE item_id IN ($1, $2) GROUP BY profile_id
			ORDER BY profile_id
			ON CONFLICT (profile_id, item_id) DO UPDATE SET position_ms = excluded.position_ms, plays = excluded.plays,
				watched_at = excluded.watched_at, last_played_at = excluded.last_played_at
			WHERE (watch_state.position_ms, watch_state.plays, watch_state.watched_at, watch_state.last_played_at)
				IS DISTINCT FROM (excluded.position_ms, excluded.plays, excluded.watched_at, excluded.last_played_at)`, id, other)
		for _, table := range []string{"watchlist", "favourites"} {
			b.Queue(`
				INSERT INTO `+table+` (profile_id, item_id, added_at) SELECT profile_id, $1, added_at FROM `+table+`
				WHERE item_id = $2 ORDER BY profile_id
				ON CONFLICT (profile_id, item_id) DO UPDATE SET added_at = least(`+table+`.added_at, excluded.added_at)`, id, other)
		}
		b.Queue(`
			INSERT INTO collection_members (collection_id, item_id, position)
			SELECT m.collection_id, $1, m.position FROM collection_members m JOIN collections c ON c.item_id = m.collection_id
			WHERE m.item_id = $2 AND c.origin = 'user' ORDER BY m.collection_id
			ON CONFLICT (collection_id, item_id) DO NOTHING`, id, other)
		for _, table := range []string{"playlist_entries", "plays", "downloads", "requests"} {
			b.Queue(`UPDATE `+table+` SET item_id = $1 WHERE item_id = $2`, id, other)
		}
		b.Queue(`DELETE FROM items WHERE id = $1`, other)
		if err := tx.SendBatch(ctx, b).Close(); err != nil {
			return err
		}
		return keyTitle(ctx, tx, id)
	})
}
