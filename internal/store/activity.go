package store

import (
	"context"
	"encoding/json"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
)

// AddActivity keeps an event in the activity log and answers its entry's id. A profile, title or
// library removed while the event was on its way is left out, as removing it later would.
func (s *Store) AddActivity(ctx context.Context, e domain.Event) (uuid.UUID, error) {
	details := []byte("{}")
	if e.Details != nil {
		var err error
		if details, err = json.Marshal(e.Details); err != nil {
			return uuid.UUID{}, err
		}
	}
	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `
		INSERT INTO activity (at, kind, profile_id, item_id, library_id, details) VALUES ($1, $2,
			(SELECT id FROM profiles WHERE id = $3), (SELECT id FROM items WHERE id = $4),
			(SELECT id FROM libraries WHERE id = $5), $6::jsonb)
		RETURNING id`,
		e.At, e.Kind, e.Profile, e.Item, e.Library, string(details)).Scan(&id)
	return id, err
}

// Activity answers a page of the activity log, the newest first, and how many entries there are:
// every kind's, or one's.
func (s *Store) Activity(ctx context.Context, kind domain.EventKind, offset, limit int) ([]domain.Event, int64, error) {
	const of = `FROM activity WHERE $1 = '' OR kind = $1`
	var total int64
	if err := s.pool.QueryRow(ctx, `SELECT count(*) `+of, kind).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := queryRows[model.Activity](ctx, s.pool, `
		SELECT id, at, kind, profile_id, item_id, library_id, details `+of+` ORDER BY at DESC, id DESC OFFSET $2 LIMIT $3`,
		kind, offset, limit)
	if err != nil {
		return nil, 0, err
	}
	out := make([]domain.Event, len(rows))
	for i, r := range rows {
		out[i] = domain.Event{
			ID: r.ID, Kind: r.Kind, At: r.At,
			Profile: deref(r.ProfileID), Item: deref(r.ItemID), Library: deref(r.LibraryID),
		}
		var err error
		if out[i].Details, err = domain.DetailsOf(r.Kind, r.Details); err != nil {
			return nil, 0, err
		}
	}
	return out, total, nil
}

// PruneActivity forgets the entries from before a time, answering how many.
func (s *Store) PruneActivity(ctx context.Context, before time.Time) (int64, error) {
	res, err := s.pool.Exec(ctx, `DELETE FROM activity WHERE at < $1`, before)
	return res.RowsAffected(), err
}

// TitlesAddedSince counts the films and episodes a library has had added since a time.
func (s *Store) TitlesAddedSince(ctx context.Context, lib uuid.UUID, since time.Time) (int64, error) {
	var n int64
	err := s.pool.QueryRow(ctx, `
		SELECT count(*) FROM items WHERE library_id = $1 AND kind IN ('movie', 'episode') AND added_at >= $2`,
		lib, since).Scan(&n)
	return n, err
}
