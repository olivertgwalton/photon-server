package store

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"gorm.io/gen/field"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/jackc/pgx/v5"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
	"github.com/olivertgwalton/photon-server/internal/store/query"
)

var (
	ErrProfileExists = errors.New("a profile with that name already exists")
	ErrNotFound      = errors.New("not found")
)

// found answers err with a row that is not there as ErrNotFound, however it was read.
func found(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func (s *Store) AddProfile(ctx context.Context, name string, role domain.Role, passwordHash string) (domain.Profile, error) {
	row := model.Profile{Name: name, Role: role, PasswordHash: optional(passwordHash)}
	if err := s.q.Profile.WithContext(ctx).Create(&row); err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return domain.Profile{}, ErrProfileExists
		}
		return domain.Profile{}, fmt.Errorf("adding profile: %w", err)
	}
	return profile(row), nil
}

// ProfileByName returns a profile and its password hash, empty for a profile that cannot sign in
// with a password.
func (s *Store) ProfileByName(ctx context.Context, name string) (domain.Profile, string, error) {
	p := s.q.Profile
	row, err := p.WithContext(ctx).Where(p.Name.Eq(name)).Take()
	if err != nil {
		return domain.Profile{}, "", found(err)
	}
	hash := ""
	if row.PasswordHash != nil {
		hash = *row.PasswordHash
	}
	return profile(*row), hash, nil
}

// SetPasswordHash replaces a profile's stored hash, to raise its parameters on a successful
// sign-in.
func (s *Store) SetPasswordHash(ctx context.Context, profileID uuid.UUID, hash string) error {
	p := s.q.Profile
	_, err := p.WithContext(ctx).Where(p.ID.Eq(model.UUID(profileID))).UpdateSimple(p.PasswordHash.Value(hash))
	return err
}

// ChangePassword replaces a profile's password and signs out every device on the profile but keep,
// since a password changed for fear of who knows it must not leave them signed in.
func (s *Store) ChangePassword(ctx context.Context, profileID uuid.UUID, hash string, keep uuid.UUID) error {
	return s.q.Transaction(func(tx *query.Query) error {
		p, d := tx.Profile, tx.DeviceSession
		if _, err := p.WithContext(ctx).Where(p.ID.Eq(model.UUID(profileID))).UpdateSimple(p.PasswordHash.Value(hash)); err != nil {
			return err
		}
		_, err := d.WithContext(ctx).Where(d.ProfileID.Eq(model.UUID(profileID)), d.ID.Neq(model.UUID(keep))).Delete()
		return err
	})
}

type NewSession struct {
	ProfileID  uuid.UUID
	TokenHash  []byte
	DeviceName string
	Client     string
	ExpiresAt  time.Time
}

func (s *Store) CreateSession(ctx context.Context, n NewSession) (uuid.UUID, error) {
	row := model.DeviceSession{
		TokenHash: n.TokenHash, ProfileID: model.UUID(n.ProfileID),
		DeviceName: n.DeviceName, Client: n.Client, ExpiresAt: n.ExpiresAt,
	}
	err := s.q.DeviceSession.WithContext(ctx).Create(&row)
	return uuid.UUID(row.ID), err
}

// SessionByToken finds the unexpired session holding a token, with when it was last seen.
func (s *Store) SessionByToken(ctx context.Context, tokenHash []byte, now time.Time) (domain.Session, time.Time, error) {
	d, p := s.q.DeviceSession, s.q.Profile
	var row struct {
		ID, ProfileID      model.UUID
		Name               string
		Role               domain.Role
		DeviceName, Client string
		LastSeenAt         time.Time
	}
	err := d.WithContext(ctx).Select(d.ID, d.ProfileID, p.Name, p.Role, d.DeviceName, d.Client, d.LastSeenAt).
		Join(p, p.ID.EqCol(d.ProfileID)).
		Where(d.TokenHash.Eq(tokenHash), d.ExpiresAt.Gt(now)).Scan(&row)
	if err != nil {
		return domain.Session{}, time.Time{}, err
	}
	if row.ID == (model.UUID{}) {
		return domain.Session{}, time.Time{}, ErrNotFound
	}
	return domain.Session{
		ID:      uuid.UUID(row.ID),
		Profile: domain.Profile{ID: uuid.UUID(row.ProfileID), Name: row.Name, Role: row.Role},
		Device:  row.DeviceName, Client: row.Client,
	}, row.LastSeenAt, nil
}

// TouchSession records a session's use and slides its expiry.
func (s *Store) TouchSession(ctx context.Context, id uuid.UUID, now, expires time.Time) error {
	d := s.q.DeviceSession
	_, err := d.WithContext(ctx).Where(d.ID.Eq(model.UUID(id))).
		UpdateSimple(d.LastSeenAt.Value(now), d.ExpiresAt.Value(expires))
	return err
}

func (s *Store) DeleteSession(ctx context.Context, id uuid.UUID) error {
	d := s.q.DeviceSession
	_, err := d.WithContext(ctx).Where(d.ID.Eq(model.UUID(id))).Delete()
	return err
}

func profile(r model.Profile) domain.Profile {
	return domain.Profile{ID: uuid.UUID(r.ID), Name: r.Name, Role: r.Role}
}

func (s *Store) ProfileByID(ctx context.Context, id uuid.UUID) (domain.Profile, error) {
	p := s.q.Profile
	row, err := p.WithContext(ctx).Where(p.ID.Eq(model.UUID(id))).Take()
	if err != nil {
		return domain.Profile{}, found(err)
	}
	return profile(*row), nil
}

// Secrets are a profile's stored hashes, empty where unset.
type Secrets struct {
	Password string
	PIN      string
}

type ProfileListing struct {
	Profile domain.Profile
	Lock    domain.ProfileLock
}

func (s *Store) Profiles(ctx context.Context) ([]ProfileListing, error) {
	p := s.q.Profile
	rows, err := p.WithContext(ctx).Order(p.Name).Find()
	if err != nil {
		return nil, err
	}
	out := make([]ProfileListing, len(rows))
	for i, r := range rows {
		out[i] = ProfileListing{Profile: profile(*r), Lock: domain.Lock(r.Role, r.PinHash != nil)}
	}
	return out, nil
}

func (s *Store) ProfileSecrets(ctx context.Context, id uuid.UUID) (domain.Profile, Secrets, error) {
	p := s.q.Profile
	row, err := p.WithContext(ctx).Where(p.ID.Eq(model.UUID(id))).Take()
	if err != nil {
		return domain.Profile{}, Secrets{}, found(err)
	}
	var sec Secrets
	if row.PasswordHash != nil {
		sec.Password = *row.PasswordHash
	}
	if row.PinHash != nil {
		sec.PIN = *row.PinHash
	}
	return profile(*row), sec, nil
}

// SetPINHash sets a profile's PIN, or clears it when hash is empty.
func (s *Store) SetPINHash(ctx context.Context, id uuid.UUID, hash string) error {
	p := s.q.Profile
	_, err := p.WithContext(ctx).Where(p.ID.Eq(model.UUID(id))).UpdateSimple(nullable(p.PinHash, hash))
	return err
}

// SetSessionProfile moves a device's session to another profile.
func (s *Store) SetSessionProfile(ctx context.Context, session, profile uuid.UUID) error {
	d := s.q.DeviceSession
	_, err := d.WithContext(ctx).Where(d.ID.Eq(model.UUID(session))).UpdateSimple(d.ProfileID.Value(model.UUID(profile)))
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
	d, p := s.q.DeviceSession, s.q.Profile
	q := d.WithContext(ctx).Select(d.ID, d.DeviceName, d.Client, p.Name.As("profile"), d.CreatedAt, d.LastSeenAt).
		Join(p, p.ID.EqCol(d.ProfileID)).Where(d.ExpiresAt.Gt(time.Now())).Order(d.LastSeenAt.Desc())
	if profile != nil {
		q = q.Where(d.ProfileID.Eq(model.UUID(*profile)))
	}
	var rows []struct {
		ID                    model.UUID
		DeviceName, Client    string
		Profile               string
		CreatedAt, LastSeenAt time.Time
	}
	if err := q.Scan(&rows); err != nil {
		return nil, err
	}
	out := make([]DeviceListing, len(rows))
	for i, r := range rows {
		out[i] = DeviceListing{
			ID: uuid.UUID(r.ID), DeviceName: r.DeviceName, Client: r.Client, Profile: r.Profile,
			CreatedAt: r.CreatedAt, LastSeenAt: r.LastSeenAt,
		}
	}
	return out, nil
}

// DeleteDevice signs a device out: any device, or with profile set only one on that profile. It
// reports whether there was such a device.
func (s *Store) DeleteDevice(ctx context.Context, id uuid.UUID, profile *uuid.UUID) (bool, error) {
	d := s.q.DeviceSession
	q := d.WithContext(ctx).Where(d.ID.Eq(model.UUID(id)))
	if profile != nil {
		q = q.Where(d.ProfileID.Eq(model.UUID(*profile)))
	}
	info, err := q.Delete()
	return info.RowsAffected == 1, err
}

var (
	// ErrLastAdmin is a change that would leave the server with no admin.
	ErrLastAdmin = errors.New("the server's last admin cannot stop being one")
	// ErrAdminNeedsPassword is an admin left with no password to sign in with.
	ErrAdminNeedsPassword = errors.New("an admin profile needs a password")
)

// ProfileChange is what to change about a profile; an empty name or role, or a nil hash, is left
// as it is, and an empty hash clears the password.
type ProfileChange struct {
	Name         string
	Role         domain.Role
	PasswordHash *string
}

// SetProfile renames a profile, changes its role, and sets or clears its password. The server
// keeps an admin, and every admin a password.
func (s *Store) SetProfile(ctx context.Context, id uuid.UUID, c ProfileChange) (domain.Profile, error) {
	var out domain.Profile
	err := s.q.Transaction(func(tx *query.Query) error {
		p := tx.Profile
		row, err := p.WithContext(ctx).Where(p.ID.Eq(model.UUID(id))).Take()
		if err != nil {
			return found(err)
		}
		if c.Role != "" && c.Role != domain.RoleAdmin && row.Role == domain.RoleAdmin {
			if err := otherAdmin(ctx, tx, row.ID); err != nil {
				return err
			}
		}
		row.Name, row.Role = cmp.Or(c.Name, row.Name), cmp.Or(c.Role, row.Role)
		if c.PasswordHash != nil {
			row.PasswordHash = optional(*c.PasswordHash)
		}
		if row.Role == domain.RoleAdmin && row.PasswordHash == nil {
			return ErrAdminNeedsPassword
		}
		password := p.PasswordHash.Null()
		if row.PasswordHash != nil {
			password = p.PasswordHash.Value(*row.PasswordHash)
		}
		if _, err := p.WithContext(ctx).Where(p.ID.Eq(row.ID)).
			UpdateSimple(p.Name.Value(row.Name), p.Role.Value(string(row.Role)), password); err != nil {
			return err
		}
		out = profile(*row)
		return nil
	})
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return domain.Profile{}, ErrProfileExists
	}
	return out, err
}

// RemoveProfile forgets a profile, its devices and what it has watched, answering its name. The
// server keeps an admin.
func (s *Store) RemoveProfile(ctx context.Context, id uuid.UUID) (string, error) {
	var name string
	err := s.q.Transaction(func(tx *query.Query) error {
		p := tx.Profile
		row, err := p.WithContext(ctx).Where(p.ID.Eq(model.UUID(id))).Take()
		if err != nil {
			return found(err)
		}
		if row.Role == domain.RoleAdmin {
			if err := otherAdmin(ctx, tx, row.ID); err != nil {
				return err
			}
		}
		name = row.Name
		_, err = p.WithContext(ctx).Where(p.ID.Eq(row.ID)).Delete()
		return err
	})
	return name, err
}

// otherAdmin answers ErrLastAdmin unless an admin besides id remains, holding every admin's row
// until the transaction ends so two admins cannot each demote the other at once.
func otherAdmin(ctx context.Context, tx *query.Query, id model.UUID) error {
	p := tx.Profile
	admins, err := p.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
		Where(p.Role.Eq(string(domain.RoleAdmin))).Find()
	if err != nil {
		return err
	}
	for _, a := range admins {
		if a.ID != id {
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
	p, pl := s.q.Profile, s.q.ProfileLibrary
	row, err := p.WithContext(ctx).Where(p.ID.Eq(model.UUID(id))).Take()
	if err != nil {
		return ProfileAccess{}, found(err)
	}
	out := ProfileAccess{Unrated: row.Unrated, Libraries: []uuid.UUID{}}
	if row.MaxAge != nil {
		age := int(*row.MaxAge)
		out.MaxAge = &age
	}
	libs, err := pl.WithContext(ctx).Where(pl.ProfileID.Eq(row.ID)).Find()
	for _, l := range libs {
		out.Libraries = append(out.Libraries, uuid.UUID(l.LibraryID))
	}
	return out, err
}

// SetAccess replaces what a profile may see. ErrNotFound for no profile, or a library there is not.
func (s *Store) SetAccess(ctx context.Context, id uuid.UUID, a ProfileAccess) error {
	return s.q.Transaction(func(tx *query.Query) error {
		p, pl := tx.Profile, tx.ProfileLibrary
		set := []field.AssignExpr{p.Unrated.Value(string(cmp.Or(a.Unrated, domain.UnratedAllow))), p.MaxAge.Null()}
		if a.MaxAge != nil {
			set[1] = p.MaxAge.Value(int16(*a.MaxAge))
		}
		res, err := p.WithContext(ctx).Where(p.ID.Eq(model.UUID(id))).UpdateSimple(set...)
		if err == nil && res.RowsAffected == 0 {
			err = ErrNotFound
		}
		if err != nil {
			return err
		}
		if _, err := pl.WithContext(ctx).Where(pl.ProfileID.Eq(model.UUID(id))).Delete(); err != nil {
			return err
		}
		for _, lib := range a.Libraries {
			err := pl.WithContext(ctx).Create(&model.ProfileLibrary{ProfileID: model.UUID(id), LibraryID: model.UUID(lib)})
			if errors.Is(err, gorm.ErrForeignKeyViolated) {
				return ErrNotFound
			}
			if err != nil {
				return err
			}
		}
		return nil
	})
}
