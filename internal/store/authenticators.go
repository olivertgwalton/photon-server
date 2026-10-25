package store

import (
	"context"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
)

// Authenticator is a profile's authenticator app: the secret its codes are made from, and the last
// step a code was taken for.
type Authenticator struct {
	Secret  []byte
	Step    int64
	AddedAt time.Time
}

// Authenticator is a profile's authenticator, of those a manager by keeps when by is set.
func (s *Store) Authenticator(ctx context.Context, profile uuid.UUID, by *uuid.UUID) (Authenticator, error) {
	return readRow[Authenticator](ctx, s.pool, `
		SELECT a.secret, a.step, a.added_at FROM authenticators a JOIN profiles ON profiles.id = a.profile_id
		WHERE a.profile_id = $1 AND `+keptBy, profile, by)
}

// SetAuthenticator gives a profile an authenticator, in place of any it had, whose code for step
// was just taken.
func (s *Store) SetAuthenticator(ctx context.Context, profile uuid.UUID, secret []byte, step int64) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO authenticators (profile_id, secret, step) VALUES ($1, $2, $3)
		ON CONFLICT (profile_id) DO UPDATE SET secret = $2, step = $3, added_at = now()`, profile, secret, step)
	return err
}

// TakeAuthenticatorStep records a code taken for step, ErrNotFound where one was for it or later.
func (s *Store) TakeAuthenticatorStep(ctx context.Context, profile uuid.UUID, step int64) error {
	return affected(s.pool.Exec(ctx, `UPDATE authenticators SET step = $2 WHERE profile_id = $1 AND step < $2`, profile, step))
}

// RemoveAuthenticator takes a profile's authenticator away, of those a manager by keeps when by is
// set.
func (s *Store) RemoveAuthenticator(ctx context.Context, profile uuid.UUID, by *uuid.UUID) error {
	return affected(s.pool.Exec(ctx, `
		DELETE FROM authenticators a USING profiles
		WHERE profiles.id = a.profile_id AND a.profile_id = $1 AND `+keptBy, profile, by))
}

// ResetPassword sets a forgotten password, signing out the profile's every device and taking its
// authenticator away: whoever reset it reads the server's log, so runs the server anyway, and may
// have lost the phone with the password.
func (s *Store) ResetPassword(ctx context.Context, profile uuid.UUID, hash string) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := changePassword(ctx, tx, profile, hash, uuid.UUID{}); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `DELETE FROM authenticators WHERE profile_id = $1`, profile)
		return err
	})
}
