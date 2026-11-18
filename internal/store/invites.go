package store

import (
	"context"
	"errors"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
)

var (
	// ErrHasPassword is inviting a profile that has a password: an invite chooses a first one.
	ErrHasPassword = errors.New("the profile has a password; an invite is for a profile without one")
	// ErrInviteNotFound is a code no invite has, one lapsed, or one whose profile has since been
	// given a password.
	ErrInviteNotFound = errors.New("no invite is waiting for that code")
)

// AddInvite keeps an invite to profile id until expires, for a profile by keeps that has no
// password. Invites lapsed by now are forgotten as it is kept.
func (s *Store) AddInvite(ctx context.Context, id uuid.UUID, by *uuid.UUID, codeHash []byte, now, expires time.Time) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var hasPassword bool
		err := tx.QueryRow(ctx, `SELECT password_hash IS NOT NULL FROM profiles WHERE id = $1 AND `+keptBy, id, by).Scan(&hasPassword)
		if err != nil {
			return found(err)
		}
		if hasPassword {
			return ErrHasPassword
		}
		if _, err := tx.Exec(ctx, `DELETE FROM invites WHERE expires_at <= $1`, now); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO invites (code_hash, profile_id, expires_at) VALUES ($1, $2, $3)`, codeHash, id, expires)
		return err
	})
}

// AcceptInvite gives the profile an unlapsed invite is for its first password, answering the
// profile. Every invite to it ends with it.
func (s *Store) AcceptInvite(ctx context.Context, codeHash []byte, passwordHash string, now time.Time) (domain.Profile, error) {
	var row model.Profile
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		row, err = readRow[model.Profile](ctx, tx, `
			UPDATE profiles SET password_hash = $3
			WHERE password_hash IS NULL AND id = (SELECT profile_id FROM invites WHERE code_hash = $1 AND expires_at > $2)
			RETURNING `+profileColumns, codeHash, now, passwordHash)
		if errors.Is(err, ErrNotFound) {
			return ErrInviteNotFound
		}
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `DELETE FROM invites WHERE profile_id = $1`, row.ID)
		return err
	})
	if err != nil {
		return domain.Profile{}, err
	}
	return profile(row), nil
}
