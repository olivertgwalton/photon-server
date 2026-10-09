package store

import (
	"context"
	"slices"
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

// TrackerChange is a film or episode a profile marked watched, or unwatched, as trackers know it:
// a film by its ids, an episode by its show's and its numbers.
type TrackerChange struct {
	Kind domain.ItemKind
	// IDs are the film's, or the episode's show's.
	IDs             map[domain.Provider]string
	Season, Episode int
	// WatchedAt is when it was watched; nil is unwatched.
	WatchedAt *time.Time
}

// TrackerChanges are what one account is yet to tell its tracker, oldest first, and the outbox's
// rows they are.
type TrackerChanges struct {
	Profile uuid.UUID
	Tracker domain.Tracker
	Rows    []int64
	Changes []TrackerChange
}

// ClaimTrackerChanges claims up to limit changes queued before settled and not claimed by another
// node, for lease, and answers them by account.
func (s *Store) ClaimTrackerChanges(ctx context.Context, settled time.Time, lease time.Duration, limit int) ([]TrackerChanges, error) {
	rows, err := s.pool.Query(ctx, `
		WITH claimed AS (
			UPDATE tracker_outbox SET claimed_until = now() + $2::interval
			WHERE id IN (
				SELECT id FROM tracker_outbox
				WHERE queued_at < $1 AND (claimed_until IS NULL OR claimed_until < now())
				ORDER BY id LIMIT $3 FOR UPDATE SKIP LOCKED)
			RETURNING id, profile_id, tracker, item_id, watched_at)
		SELECT c.id, c.profile_id, c.tracker, c.watched_at, i.kind,
			CASE WHEN i.kind = 'episode' THEN CASE WHEN p.kind = 'season' THEN p.parent_id ELSE p.id END ELSE i.id END,
			coalesce(i.season_number, 0), coalesce(i.episode_number, 0)
		FROM claimed c JOIN items i ON i.id = c.item_id LEFT JOIN items p ON p.id = i.parent_id
		ORDER BY c.id`, settled, lease, limit)
	if err != nil {
		return nil, err
	}
	type claimedRow struct {
		id      int64
		account TrackerChanges
		owner   uuid.UUID
		change  TrackerChange
	}
	claimed, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (claimedRow, error) {
		var r claimedRow
		return r, row.Scan(&r.id, &r.account.Profile, &r.account.Tracker, &r.change.WatchedAt, &r.change.Kind,
			&r.owner, &r.change.Season, &r.change.Episode)
	})
	if err != nil || len(claimed) == 0 {
		return nil, err
	}
	owners := make([]uuid.UUID, len(claimed))
	for i, r := range claimed {
		owners[i] = r.owner
	}
	ids, err := s.ExternalIDs(ctx, owners)
	if err != nil {
		return nil, err
	}
	var out []TrackerChanges
	for _, r := range claimed {
		r.change.IDs = ids[r.owner]
		i := slices.IndexFunc(out, func(a TrackerChanges) bool { return a.Profile == r.account.Profile && a.Tracker == r.account.Tracker })
		if i < 0 {
			out, i = append(out, r.account), len(out)
		}
		out[i].Rows = append(out[i].Rows, r.id)
		out[i].Changes = append(out[i].Changes, r.change)
	}
	return out, nil
}

// ForgetTrackerChanges forgets changes told.
func (s *Store) ForgetTrackerChanges(ctx context.Context, rows []int64) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM tracker_outbox WHERE id = ANY($1)`, rows)
	return err
}

// ForgetTrackerWatched forgets that a profile's account is yet to tell its tracker a title was
// watched, as the play's scrobble told it.
func (s *Store) ForgetTrackerWatched(ctx context.Context, profile uuid.UUID, t domain.Tracker, item uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `
		DELETE FROM tracker_outbox WHERE profile_id = $1 AND tracker = $2 AND watched_at IS NOT NULL
			AND item_id IN (SELECT same_title($3))`, profile, t, item)
	return err
}
