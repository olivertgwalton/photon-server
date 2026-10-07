package store

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
)

var (
	ErrProfileExists = errors.New("a profile with that name already exists")
	ErrNotFound      = errors.New("not found")
)

// profileColumns are model.Profile's, for a statement that reads whole profiles.
const profileColumns = `id, name, role, password_hash, pin_hash, avatar_id`

func (s *Store) AddProfile(ctx context.Context, name string, role domain.Role, passwordHash string) (domain.Profile, error) {
	row := model.Profile{Name: name, Role: role, PasswordHash: passwordHash}
	err := s.pool.QueryRow(ctx, `INSERT INTO profiles (name, role, password_hash) VALUES ($1, $2, $3) RETURNING id`,
		row.Name, row.Role, row.PasswordHash).Scan(&row.ID)
	if violates(err, uniqueViolation) {
		return domain.Profile{}, ErrProfileExists
	}
	if err != nil {
		return domain.Profile{}, fmt.Errorf("adding profile: %w", err)
	}
	return profile(row), nil
}

// ProfileByName returns a profile and its password hash, its name matched in any case.
func (s *Store) ProfileByName(ctx context.Context, name string) (domain.Profile, string, error) {
	row, err := readRow[model.Profile](ctx, s.pool, `SELECT `+profileColumns+` FROM profiles WHERE lower(name) = lower($1)`, name)
	if err != nil {
		return domain.Profile{}, "", err
	}
	return profile(row), row.PasswordHash, nil
}

// SetPasswordHash replaces a profile's stored hash, to raise its parameters on a successful
// sign-in.
func (s *Store) SetPasswordHash(ctx context.Context, profileID uuid.UUID, hash string) error {
	_, err := s.pool.Exec(ctx, `UPDATE profiles SET password_hash = $2 WHERE id = $1`, profileID, hash)
	return err
}

// ChangePassword replaces a profile's password and signs out every device on the profile but keep,
// since a password changed for fear of who knows it must not leave them signed in. Its API keys
// stay, as Jellyfin's do: they are not the password, and revoking one is its own act.
func (s *Store) ChangePassword(ctx context.Context, profileID uuid.UUID, hash string, keep uuid.UUID) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE profiles SET password_hash = $2 WHERE id = $1`, profileID, hash); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `DELETE FROM device_sessions WHERE profile_id = $1 AND id <> $2 AND kind = 'device'`, profileID, keep)
		return err
	})
}

type NewSession struct {
	Kind       domain.SessionKind
	ProfileID  uuid.UUID
	TokenHash  []byte
	DeviceName string
	Client     string
	// ExpiresAt is nil for a key.
	ExpiresAt *time.Time
}

func (s *Store) CreateSession(ctx context.Context, n NewSession) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `
		INSERT INTO device_sessions (kind, token_hash, profile_id, device_name, client, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
		n.Kind, n.TokenHash, n.ProfileID, n.DeviceName, n.Client, n.ExpiresAt).Scan(&id)
	return id, err
}

// SessionByToken finds the unexpired session holding a token, with when it was last seen.
func (s *Store) SessionByToken(ctx context.Context, tokenHash []byte, now time.Time) (domain.Session, time.Time, error) {
	var (
		id         uuid.UUID
		kind       domain.SessionKind
		p          model.Profile
		device     string
		client     string
		lastSeenAt time.Time
	)
	err := s.pool.QueryRow(ctx, `
		SELECT d.id, d.kind, d.profile_id, p.name, p.role, p.avatar_id, d.device_name, d.client, d.last_seen_at
		FROM device_sessions d JOIN profiles p ON p.id = d.profile_id
		WHERE d.token_hash = $1 AND (d.expires_at IS NULL OR d.expires_at > $2)`, tokenHash, now).
		Scan(&id, &kind, &p.ID, &p.Name, &p.Role, &p.AvatarID, &device, &client, &lastSeenAt)
	if err != nil {
		return domain.Session{}, time.Time{}, found(err)
	}
	return domain.Session{ID: id, Kind: kind, Profile: profile(p), Device: device, Client: client}, lastSeenAt, nil
}

// TouchSession records a session's use and slides its expiry, which a key has none of.
func (s *Store) TouchSession(ctx context.Context, id uuid.UUID, now time.Time, expires *time.Time) error {
	_, err := s.pool.Exec(ctx, `UPDATE device_sessions SET last_seen_at = $2, expires_at = $3 WHERE id = $1`, id, now, expires)
	return err
}

func (s *Store) DeleteSession(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM device_sessions WHERE id = $1`, id)
	return err
}

func profile(r model.Profile) domain.Profile {
	return domain.Profile{ID: r.ID, Name: r.Name, Role: r.Role, Avatar: deref(r.AvatarID)}
}

func (s *Store) ProfileByID(ctx context.Context, id uuid.UUID) (domain.Profile, error) {
	row, err := readRow[model.Profile](ctx, s.pool, `SELECT `+profileColumns+` FROM profiles WHERE id = $1`, id)
	if err != nil {
		return domain.Profile{}, err
	}
	return profile(row), nil
}

// Secrets are a profile's stored hashes, the PIN's empty where unset.
type Secrets struct {
	Password string
	PIN      string
}

type ProfileListing struct {
	Profile domain.Profile
	Lock    domain.ProfileLock
}

func (s *Store) Profiles(ctx context.Context) ([]ProfileListing, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+profileColumns+` FROM profiles ORDER BY name`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (ProfileListing, error) {
		p, err := pgx.RowToStructByName[model.Profile](r)
		return ProfileListing{Profile: profile(p), Lock: domain.Lock(p.Role, p.PinHash != nil)}, err
	})
}

func (s *Store) ProfileSecrets(ctx context.Context, id uuid.UUID) (domain.Profile, Secrets, error) {
	row, err := readRow[model.Profile](ctx, s.pool, `SELECT `+profileColumns+` FROM profiles WHERE id = $1`, id)
	if err != nil {
		return domain.Profile{}, Secrets{}, err
	}
	return profile(row), Secrets{Password: row.PasswordHash, PIN: deref(row.PinHash)}, nil
}

// SetPINHash sets a profile's PIN, or clears it when hash is empty.
func (s *Store) SetPINHash(ctx context.Context, id uuid.UUID, hash string) error {
	_, err := s.pool.Exec(ctx, `UPDATE profiles SET pin_hash = $2 WHERE id = $1`, id, optional(hash))
	return err
}

// SetSessionProfile moves a device's session to another profile.
func (s *Store) SetSessionProfile(ctx context.Context, session, profile uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `UPDATE device_sessions SET profile_id = $2 WHERE id = $1`, session, profile)
	return err
}

type DeviceListing struct {
	ID         uuid.UUID
	DeviceName string
	Client     string
	Profile    string
	CreatedAt  time.Time
	LastSeenAt time.Time
}

// Devices lists signed-in devices, newest first: every device, or with profile set only those on
// that profile.
func (s *Store) Devices(ctx context.Context, profile *uuid.UUID) ([]DeviceListing, error) {
	return queryStructs[DeviceListing](ctx, s.pool, `
		SELECT d.id, d.device_name, d.client, p.name AS profile, d.created_at, d.last_seen_at
		FROM device_sessions d JOIN profiles p ON p.id = d.profile_id
		WHERE d.kind = 'device' AND d.expires_at > now() AND ($1::uuid IS NULL OR d.profile_id = $1)
		ORDER BY d.last_seen_at DESC`, profile)
}

// DeleteDevice signs a device out: any device, or with profile set only one on that profile. It
// reports whether there was such a device.
func (s *Store) DeleteDevice(ctx context.Context, id uuid.UUID, profile *uuid.UUID) (bool, error) {
	info, err := s.pool.Exec(ctx, `
		DELETE FROM device_sessions WHERE id = $1 AND kind = 'device' AND ($2::uuid IS NULL OR profile_id = $2)`, id, profile)
	return info.RowsAffected() == 1, err
}

type KeyListing struct {
	ID   uuid.UUID
	Name string
	// Profile is the admin who made it, whom it acts as.
	Profile    string
	CreatedAt  time.Time
	LastSeenAt time.Time
}

// Keys lists the API keys, newest first.
func (s *Store) Keys(ctx context.Context) ([]KeyListing, error) {
	return queryStructs[KeyListing](ctx, s.pool, `
		SELECT d.id, d.device_name AS name, p.name AS profile, d.created_at, d.last_seen_at
		FROM device_sessions d JOIN profiles p ON p.id = d.profile_id
		WHERE d.kind = 'key'
		ORDER BY d.created_at DESC`)
}

// DeleteKey revokes an API key, reporting whether there was one.
func (s *Store) DeleteKey(ctx context.Context, id uuid.UUID) (bool, error) {
	info, err := s.pool.Exec(ctx, `DELETE FROM device_sessions WHERE id = $1 AND kind = 'key'`, id)
	return info.RowsAffected() == 1, err
}

// ErrLastAdmin is a change that would leave the server with no admin.
var ErrLastAdmin = errors.New("the server's last admin cannot stop being one")

// ProfileChange is what to change about a profile; an empty field is left as it is.
type ProfileChange struct {
	Name         string
	Role         domain.Role
	PasswordHash string
}

// SetProfile renames a profile, changes its role, and sets its password. The server keeps an
// admin.
func (s *Store) SetProfile(ctx context.Context, id uuid.UUID, c ProfileChange) (domain.Profile, error) {
	var out domain.Profile
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		row, err := readRow[model.Profile](ctx, tx, `SELECT `+profileColumns+` FROM profiles WHERE id = $1`, id)
		if err != nil {
			return err
		}
		if c.Role != "" && c.Role != domain.RoleAdmin && row.Role == domain.RoleAdmin {
			if err := otherAdmin(ctx, tx, row.ID); err != nil {
				return err
			}
		}
		row.Name, row.Role = cmp.Or(c.Name, row.Name), cmp.Or(c.Role, row.Role)
		row.PasswordHash = cmp.Or(c.PasswordHash, row.PasswordHash)
		if _, err := tx.Exec(ctx, `UPDATE profiles SET name = $2, role = $3, password_hash = $4 WHERE id = $1`,
			row.ID, row.Name, row.Role, row.PasswordHash); err != nil {
			return err
		}
		out = profile(row)
		return nil
	})
	if violates(err, uniqueViolation) {
		return domain.Profile{}, ErrProfileExists
	}
	return out, err
}

// RemoveProfile forgets a profile, its devices and what it has watched, answering its name. The
// server keeps an admin.
func (s *Store) RemoveProfile(ctx context.Context, id uuid.UUID) (string, error) {
	var name string
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var role domain.Role
		if err := tx.QueryRow(ctx, `SELECT name, role FROM profiles WHERE id = $1`, id).Scan(&name, &role); err != nil {
			return found(err)
		}
		if role == domain.RoleAdmin {
			if err := otherAdmin(ctx, tx, id); err != nil {
				return err
			}
		}
		_, err := tx.Exec(ctx, `DELETE FROM profiles WHERE id = $1`, id)
		return err
	})
	return name, err
}

// otherAdmin answers ErrLastAdmin unless an admin besides id remains, holding every admin's row
// until the transaction ends so two admins cannot each demote the other at once.
func otherAdmin(ctx context.Context, tx db, id uuid.UUID) error {
	admins, err := queryColumn[uuid.UUID](ctx, tx, `SELECT id FROM profiles WHERE role = $1 FOR UPDATE`, domain.RoleAdmin)
	if err != nil {
		return err
	}
	for _, a := range admins {
		if a != id {
			return nil
		}
	}
	return ErrLastAdmin
}

// ProfileAccess is what a profile may see: titles rated for MaxAge and younger (nil for any), unrated
// ones as Unrated says where there is an age, and only Libraries (none for every library).
type ProfileAccess struct {
	MaxAge    *int
	Unrated   domain.Unrated
	Libraries []uuid.UUID
}

// Access answers what a profile may see.
func (s *Store) Access(ctx context.Context, id uuid.UUID) (ProfileAccess, error) {
	var out ProfileAccess
	err := s.pool.QueryRow(ctx, `
		SELECT p.max_age, p.unrated,
			array(SELECT l.library_id FROM profile_libraries l WHERE l.profile_id = p.id)
		FROM profiles p WHERE p.id = $1`, id).Scan(&out.MaxAge, &out.Unrated, &out.Libraries)
	if out.Libraries == nil {
		out.Libraries = []uuid.UUID{}
	}
	return out, found(err)
}

// SetAccess replaces what a profile may see. ErrNotFound for no profile, or a library there is not.
func (s *Store) SetAccess(ctx context.Context, id uuid.UUID, a ProfileAccess) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := affected(tx.Exec(ctx, `UPDATE profiles SET unrated = $2, max_age = $3 WHERE id = $1`,
			id, cmp.Or(a.Unrated, domain.UnratedAllow), a.MaxAge)); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM profile_libraries WHERE profile_id = $1`, id); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO profile_libraries (profile_id, library_id) SELECT $1, unnest($2::uuid[])`, id, a.Libraries)
		if violates(err, foreignKeyViolation) {
			return ErrNotFound
		}
		return err
	})
}

// SetAvatar makes picture a profile's avatar, or with the zero id takes it away, answering the
// profile as it is then. The picture it had is forgotten, and its file swept with the rest.
func (s *Store) SetAvatar(ctx context.Context, id, picture uuid.UUID) (domain.Profile, error) {
	var avatar *uuid.UUID
	if picture != (uuid.UUID{}) {
		avatar = &picture
	}
	if err := affected(s.pool.Exec(ctx, `UPDATE profiles SET avatar_id = $2 WHERE id = $1`, id, avatar)); err != nil {
		return domain.Profile{}, err
	}
	return s.ProfileByID(ctx, id)
}
