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
