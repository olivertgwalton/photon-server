package store

import (
	"context"
	"encoding/json"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

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

// Activity answers up to limit entries of the activity log from before, the newest first, and
// whether there are more: every kind's, or one's. Each is its own statement, as either's index reads
// it in order.
func (s *Store) Activity(ctx context.Context, kind domain.EventKind, before Before, limit int) ([]domain.Event, bool, error) {
	const columns = `SELECT id, at, kind, profile_id, item_id, library_id, details FROM activity`
	const page = ` (at, id) < (@at, @id) ORDER BY at DESC, id DESC LIMIT @limit`
	sql := columns + ` WHERE` + page
	if kind != "" {
		sql = columns + ` WHERE kind = @kind AND` + page
	}
	read, err := queryRows[model.Activity](ctx, s.pool, sql, pgx.NamedArgs{"kind": kind, "at": before.At, "id": before.ID, "limit": limit + 1})
	if err != nil {
		return nil, false, err
	}
	rows, more := pageOf(read, limit)
	out := make([]domain.Event, len(rows))
	for i, r := range rows {
		out[i] = domain.Event{
			ID: r.ID, Kind: r.Kind, At: r.At,
			Profile: deref(r.ProfileID), Item: deref(r.ItemID), Library: deref(r.LibraryID),
		}
		var err error
		if out[i].Details, err = domain.DetailsOf(r.Kind, r.Details); err != nil {
			return nil, false, err
		}
	}
	return out, more, nil
}

// PruneActivity forgets the entries from before a time, answering how many.
func (s *Store) PruneActivity(ctx context.Context, before time.Time) (int64, error) {
	res, err := s.pool.Exec(ctx, `DELETE FROM activity WHERE at < $1`, before)
	return res.RowsAffected(), err
}

// PruneExpiredDevices forgets the devices whose sign-in has expired, with what they downloaded,
// answering how many.
func (s *Store) PruneExpiredDevices(ctx context.Context) (int64, error) {
	res, err := s.pool.Exec(ctx, `DELETE FROM device_sessions WHERE kind = 'device' AND expires_at < now()`)
	return res.RowsAffected(), err
}

// PruneFailedDeliveries forgets the webhook deliveries that failed for good before a time, answering
// how many, and the failed jobs of webhooks since removed.
func (s *Store) PruneFailedDeliveries(ctx context.Context, before time.Time) (int64, error) {
	res, err := s.pool.Exec(ctx, `
		WITH dead AS (
			DELETE FROM jobs j WHERE j.kind = 'deliver_webhook' AND j.state = 'dead'
				AND (j.run_after < $1 OR NOT EXISTS (SELECT FROM webhook_deliveries d WHERE d.id = j.subject))
			RETURNING j.subject)
		DELETE FROM webhook_deliveries d USING dead WHERE d.id = dead.subject`, before)
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
