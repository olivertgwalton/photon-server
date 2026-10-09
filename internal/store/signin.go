package store

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
)

var (
	// ErrAccountLinked is an account at a provider that another profile linked.
	ErrAccountLinked = errors.New("that account is linked to another profile: unlink it there first")
	// ErrLastWayIn is unlinking the one account a profile with no password signs in with.
	ErrLastWayIn = errors.New("the profile signs in with nothing else: set a password first")
)

const signInProviderColumns = `slug, name, issuer, client_id, client_secret, provisioning, required_group, recheck,
	max_age, unrated, array(SELECT l.library_id FROM sign_in_provider_libraries l WHERE l.provider = slug ORDER BY l.library_id)`

func scanSignInProvider(row pgx.CollectableRow) (domain.SignInProvider, error) {
	var p domain.SignInProvider
	err := row.Scan(&p.Slug, &p.Name, &p.Issuer, &p.ClientID, &p.ClientSecret, &p.Provisioning, &p.Group, &p.Recheck,
		&p.MaxAge, &p.Unrated, &p.Libraries)
	return p, err
}

// SignInProviders answers every provider the household signs in through, by name.
func (s *Store) SignInProviders(ctx context.Context) ([]domain.SignInProvider, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+signInProviderColumns+` FROM sign_in_providers ORDER BY lower(name), slug`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanSignInProvider)
}

func (s *Store) SignInProvider(ctx context.Context, slug string) (domain.SignInProvider, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+signInProviderColumns+` FROM sign_in_providers WHERE slug = $1`, slug)
	if err != nil {
		return domain.SignInProvider{}, err
	}
	p, err := pgx.CollectOneRow(rows, scanSignInProvider)
	return p, found(err)
}

// SetSignInProvider keeps a provider in place of any of its slug. One whose issuer changes forgets
// the accounts linked at the old one, whose subjects mean nothing to the new, and their sessions
// with them; one that no longer rechecks forgets the refresh tokens it would recheck by. ErrNotFound
// for a library that is not.
func (s *Store) SetSignInProvider(ctx context.Context, p domain.SignInProvider) error {
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			DELETE FROM sign_in_accounts a USING sign_in_providers p
			WHERE a.provider = p.slug AND p.slug = $1 AND p.issuer <> $2`, p.Slug, p.Issuer)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO sign_in_providers (slug, name, issuer, client_id, client_secret, provisioning, required_group,
				recheck, max_age, unrated)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
			ON CONFLICT (slug) DO UPDATE SET name = excluded.name, issuer = excluded.issuer,
				client_id = excluded.client_id, client_secret = excluded.client_secret,
				provisioning = excluded.provisioning, required_group = excluded.required_group,
				recheck = excluded.recheck, max_age = excluded.max_age, unrated = excluded.unrated`,
			p.Slug, p.Name, p.Issuer, p.ClientID, p.ClientSecret, p.Provisioning, p.Group, p.Recheck, p.MaxAge,
			cmp.Or(p.Unrated, domain.UnratedAllow))
		if err != nil {
			return err
		}
		switch p.Recheck {
		case domain.RecheckAtSignIn:
			if _, err := tx.Exec(ctx, `UPDATE sign_in_accounts SET refresh_token = NULL WHERE provider = $1`, p.Slug); err != nil {
				return err
			}
		case domain.RecheckHourly:
		}
		if _, err := tx.Exec(ctx, `DELETE FROM sign_in_provider_libraries WHERE provider = $1`, p.Slug); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO sign_in_provider_libraries (provider, library_id) SELECT $1, unnest($2::uuid[])`, p.Slug, p.Libraries)
		return err
	})
	if violates(err, foreignKeyViolation) {
		return ErrNotFound
	}
	return err
}

// RemoveSignInProvider forgets a provider and every account linked at it. ErrNotFound for none.
func (s *Store) RemoveSignInProvider(ctx context.Context, slug string) error {
	return affected(s.pool.Exec(ctx, `DELETE FROM sign_in_providers WHERE slug = $1`, slug))
}

// ProfileBySignIn answers the profile that linked a provider's account. ErrNotFound for none.
func (s *Store) ProfileBySignIn(ctx context.Context, provider, subject string) (domain.Profile, error) {
	row, err := readRow[model.Profile](ctx, s.pool, `
		SELECT `+profileColumns+` FROM profiles
		WHERE id = (SELECT profile_id FROM sign_in_accounts WHERE provider = $1 AND subject = $2)`, provider, subject)
	if err != nil {
		return domain.Profile{}, err
	}
	return profile(row), nil
}

// SignInAccounts answers the accounts a profile linked, in no order.
func (s *Store) SignInAccounts(ctx context.Context, profile uuid.UUID) ([]domain.SignInAccount, error) {
	return queryStructs[domain.SignInAccount](ctx, s.pool, `
		SELECT provider, username, linked_at FROM sign_in_accounts WHERE profile_id = $1`, profile)
}

// HasPassword is whether a profile has a password, which one a provider's account was given has
// not until it sets one.
func (s *Store) HasPassword(ctx context.Context, profile uuid.UUID) (bool, error) {
	var has bool
	err := s.pool.QueryRow(ctx, `SELECT password_hash IS NOT NULL FROM profiles WHERE id = $1`, profile).Scan(&has)
	return has, found(err)
}

// LinkSignIn keeps the account a profile linked at a provider. Linked again, it is renamed; in
// place of another the profile linked there before, the other is forgotten with its sessions.
// ErrAccountLinked for an account another profile linked, ErrNotFound for no such profile or
// provider.
func (s *Store) LinkSignIn(ctx context.Context, profile uuid.UUID, provider, subject, username string) error {
	return linked(pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE sign_in_accounts SET username = $4 WHERE profile_id = $1 AND provider = $2 AND subject = $3`,
			profile, provider, subject, username)
		if err != nil || tag.RowsAffected() == 1 {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM sign_in_accounts WHERE profile_id = $1 AND provider = $2`, profile, provider); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO sign_in_accounts (profile_id, provider, subject, username) VALUES ($1, $2, $3, $4)`,
			profile, provider, subject, username)
		return err
	}))
}

func linked(err error) error {
	switch {
	case violates(err, uniqueViolation):
		return ErrAccountLinked
	case violates(err, foreignKeyViolation):
		return ErrNotFound
	}
	return err
}

// UnlinkSignIn forgets the account a profile linked at a provider, and signs out the devices it
// signed in, but not the one way in of a profile with no password. ErrNotFound for none.
func (s *Store) UnlinkSignIn(ctx context.Context, profile uuid.UUID, provider string) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		// The profile is held, so two unlinks at once cannot each leave the other's account last.
		var password bool
		var others int
		err := tx.QueryRow(ctx, `
			SELECT p.password_hash IS NOT NULL,
				(SELECT count(*) FROM sign_in_accounts a WHERE a.profile_id = p.id AND a.provider <> $2)
			FROM profiles p WHERE p.id = $1 FOR UPDATE`, profile, provider).Scan(&password, &others)
		if err != nil {
			return found(err)
		}
		tag, err := tx.Exec(ctx, `DELETE FROM sign_in_accounts WHERE profile_id = $1 AND provider = $2`, profile, provider)
		if err := affected(tag, err); err != nil {
			return err
		}
		if !password && others == 0 {
			return ErrLastWayIn
		}
		return nil
	})
}

// AddSignInProfile adds a user the admin keeps, with no password, for an account a provider signed
// in, seeing what the provider gives the profiles it makes, and links the account to it. It is
// named name, or name and the first number after it that no profile has. ErrAccountLinked for an
// account a profile linked meanwhile.
func (s *Store) AddSignInProfile(ctx context.Context, name, provider, subject, username string) (domain.Profile, error) {
	row := model.Profile{Role: domain.RoleUser}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		// Held, so the provider is there to read each name tried from.
		if _, err := readRow[struct{ Slug string }](ctx, tx, `SELECT slug FROM sign_in_providers WHERE slug = $1 FOR SHARE`, provider); err != nil {
			return err
		}
		for n := 1; row.ID == (uuid.UUID{}); n++ {
			if n > maxNameTries {
				return fmt.Errorf("no free name like %q", name)
			}
			row.Name = numbered(name, n)
			err := tx.QueryRow(ctx, `
				INSERT INTO profiles (name, role, max_age, unrated)
				SELECT $1, $2, p.max_age, p.unrated FROM sign_in_providers p WHERE p.slug = $3
				ON CONFLICT ((lower(name))) DO NOTHING RETURNING id`, row.Name, row.Role, provider).Scan(&row.ID)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO profile_libraries (profile_id, library_id)
			SELECT $1, library_id FROM sign_in_provider_libraries WHERE provider = $2`, row.ID, provider)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO sign_in_accounts (profile_id, provider, subject, username) VALUES ($1, $2, $3, $4)`,
			row.ID, provider, subject, username)
		return linked(err)
	})
	if err != nil {
		return domain.Profile{}, err
	}
	return profile(row), nil
}

// maxNameTries is how many numbers AddSignInProfile tries after a name before giving up.
const maxNameTries = 100

// numbered is name the nth time it is tried: as it is first, then with a number after it, the name
// cut short so the number fits.
func numbered(name string, n int) string {
	if n == 1 {
		return name
	}
	suffix := " " + strconv.Itoa(n)
	runes := []rune(name)
	return string(runes[:min(len(runes), domain.MaxProfileName-len(suffix))]) + suffix
}

// KeepSignInToken keeps the refresh token an account's provider granted as it signed in, which it
// is rechecked by from now; none where the provider granted none.
func (s *Store) KeepSignInToken(ctx context.Context, id domain.SignInIdentity, token string) error {
	return affected(s.pool.Exec(ctx, `
		UPDATE sign_in_accounts SET refresh_token = $3, checked_at = now() WHERE provider = $1 AND subject = $2`,
		id.Provider, id.Subject, optional(token)))
}

// SignInCheck is an account to ask its provider after, by the refresh token it was granted.
type SignInCheck struct {
	domain.SignInIdentity
	Token string
}

// SignInChecksDue takes the accounts last checked longer ago than every, at providers that recheck
// them, as checked now: of nodes asking at once, each account goes to one.
func (s *Store) SignInChecksDue(ctx context.Context, every time.Duration) ([]SignInCheck, error) {
	rows, err := s.pool.Query(ctx, `
		UPDATE sign_in_accounts a SET checked_at = now()
		FROM sign_in_providers p
		WHERE p.slug = a.provider AND p.recheck = 'hourly' AND a.refresh_token IS NOT NULL
			AND a.checked_at < now() - make_interval(secs => $1)
		RETURNING a.provider, a.subject, a.refresh_token`, every.Seconds())
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (SignInCheck, error) {
		var c SignInCheck
		return c, row.Scan(&c.Provider, &c.Subject, &c.Token)
	})
}

// RenewSignInToken keeps the refresh token a check was granted, unless the account signed in
// meanwhile and was granted another.
func (s *Store) RenewSignInToken(ctx context.Context, c SignInCheck, token string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE sign_in_accounts SET refresh_token = $4 WHERE provider = $1 AND subject = $2 AND refresh_token = $3`,
		c.Provider, c.Subject, c.Token, token)
	return err
}

// EndSignInSessions signs out every device an account signed in, and forgets the refresh token its
// check was refused, answering whether it did: an account that signed in meanwhile, and so was let
// in since, is left as it is.
func (s *Store) EndSignInSessions(ctx context.Context, c SignInCheck) (bool, error) {
	var ended bool
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE sign_in_accounts SET refresh_token = NULL WHERE provider = $1 AND subject = $2 AND refresh_token = $3`,
			c.Provider, c.Subject, c.Token)
		if err != nil || tag.RowsAffected() == 0 {
			return err
		}
		ended = true
		_, err = tx.Exec(ctx, `DELETE FROM device_sessions WHERE sign_in_provider = $1 AND sign_in_subject = $2`, c.Provider, c.Subject)
		return err
	})
	return ended, err
}
