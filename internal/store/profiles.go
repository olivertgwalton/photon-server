package store

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"gorm.io/gorm"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
)

var (
	ErrProfileExists = errors.New("a profile with that name already exists")
	ErrNotFound      = errors.New("not found")
)

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
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return domain.Profile{}, "", ErrNotFound
	}
	if err != nil {
		return domain.Profile{}, "", err
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
		ID, ProfileID model.UUID
		Name          string
		Role          domain.Role
		LastSeenAt    time.Time
	}
	err := d.WithContext(ctx).Select(d.ID, d.ProfileID, p.Name, p.Role, d.LastSeenAt).
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
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return domain.Profile{}, ErrNotFound
	}
	if err != nil {
		return domain.Profile{}, err
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
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return domain.Profile{}, Secrets{}, ErrNotFound
	}
	if err != nil {
		return domain.Profile{}, Secrets{}, err
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
