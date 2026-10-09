package store

import (
	"context"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// TrackerTokens are what a tracker granted an account: its access token until Expires, and the
// token that refreshes it.
type TrackerTokens struct {
	Access  string
	Refresh string
	Expires time.Time
}

// TrackerClients answers the client id of the app an admin registered on each tracker that has one.
func (s *Store) TrackerClients(ctx context.Context) (map[domain.Tracker]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT tracker, client_id FROM trackers`)
	if err != nil {
		return nil, err
	}
	out := map[domain.Tracker]string{}
	var t domain.Tracker
	var id string
	_, err = pgx.ForEachRow(rows, []any{&t, &id}, func() error {
		out[t] = id
		return nil
	})
	return out, err
}

// SetTrackerClient keeps the client id of the app an admin registered on a tracker; "" forgets it.
func (s *Store) SetTrackerClient(ctx context.Context, t domain.Tracker, clientID string) error {
	if clientID == "" {
		_, err := s.pool.Exec(ctx, `DELETE FROM trackers WHERE tracker = $1`, t)
		return err
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO trackers (tracker, client_id) VALUES ($1, $2)
		ON CONFLICT (tracker) DO UPDATE SET client_id = excluded.client_id`, t, clientID)
	return err
}

// LinkTracker keeps the account a profile linked on a tracker, in place of any it linked before.
// ErrNotFound for no such profile.
func (s *Store) LinkTracker(ctx context.Context, profile uuid.UUID, t domain.Tracker, username string, tok TrackerTokens) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO tracker_accounts (profile_id, tracker, username, access_token, refresh_token, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (profile_id, tracker) DO UPDATE SET username = excluded.username,
			access_token = excluded.access_token, refresh_token = excluded.refresh_token,
			expires_at = excluded.expires_at, linked_at = now()`,
		profile, t, username, tok.Access, tok.Refresh, tok.Expires)
	if violates(err, foreignKeyViolation) {
		return ErrNotFound
	}
	return err
}

// UnlinkTracker forgets a profile's account on a tracker, answering its tokens for the tracker to
// be told. ErrNotFound for none.
func (s *Store) UnlinkTracker(ctx context.Context, profile uuid.UUID, t domain.Tracker) (TrackerTokens, error) {
	var tok TrackerTokens
	err := s.pool.QueryRow(ctx, `
		DELETE FROM tracker_accounts WHERE profile_id = $1 AND tracker = $2
		RETURNING access_token, refresh_token, expires_at`, profile, t).Scan(&tok.Access, &tok.Refresh, &tok.Expires)
	return tok, found(err)
}

// TrackerGrant is an account a profile linked, and what its tracker granted it.
type TrackerGrant struct {
	domain.TrackerAccount
	TrackerTokens
}

// TrackerGrants answers the accounts a profile linked, in no order.
func (s *Store) TrackerGrants(ctx context.Context, profile uuid.UUID) ([]TrackerGrant, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT tracker, username, linked_at, access_token, refresh_token, expires_at
		FROM tracker_accounts WHERE profile_id = $1`, profile)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (TrackerGrant, error) {
		var g TrackerGrant
		return g, row.Scan(&g.Tracker, &g.Username, &g.LinkedAt, &g.Access, &g.Refresh, &g.Expires)
	})
}

// RefreshTracker keeps what refresh answers for a profile's account on a tracker whose access
// token expires before stale, holding the account while it asks, so one node refreshes it: Trakt's
// refresh tokens are used once, and each of Simkl's refreshes ends the access token before. An
// account another node refreshed meanwhile is answered as it now is. ErrNotFound for none.
func (s *Store) RefreshTracker(ctx context.Context, profile uuid.UUID, t domain.Tracker, stale time.Time,
	refresh func(context.Context, TrackerTokens) (TrackerTokens, error),
) (TrackerTokens, error) {
	var tok TrackerTokens
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `
			SELECT access_token, refresh_token, expires_at FROM tracker_accounts
			WHERE profile_id = $1 AND tracker = $2 FOR UPDATE`, profile, t).Scan(&tok.Access, &tok.Refresh, &tok.Expires)
		if err != nil || !tok.Expires.Before(stale) {
			return err
		}
		if tok, err = refresh(ctx, tok); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			UPDATE tracker_accounts SET access_token = $3, refresh_token = $4, expires_at = $5
			WHERE profile_id = $1 AND tracker = $2`, profile, t, tok.Access, tok.Refresh, tok.Expires)
		return err
	})
	return tok, found(err)
}
