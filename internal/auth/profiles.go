package auth

import (
	"context"
	"errors"
	"strings"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

var (
	ErrWrongSecret   = errors.New("the PIN or password is wrong")
	ErrPINNotDigits  = errors.New("a PIN is 4 to 6 digits")
	ErrAdminPassword = errors.New("an admin profile needs a password")
	// ErrNoPassword is a household profile, chosen on a signed-in device: only an admin gives it
	// a password, which would let it sign in by itself.
	ErrNoPassword = errors.New("this profile has no password to change; an admin gives it one")
)

// SwitchProfile moves a signed-in device to another profile of the household, if secret is what
// that profile's lock asks for.
func (s *Service) SwitchProfile(ctx context.Context, session domain.Session, target uuid.UUID, secret string) (domain.Profile, error) {
	profile, secrets, err := s.store.ProfileSecrets(ctx, target)
	if err != nil {
		return domain.Profile{}, err
	}
	if target != session.Profile.ID {
		var hash string
		switch lock := domain.Lock(profile.Role, secrets.PIN != ""); lock {
		case domain.LockNone:
		case domain.LockPIN:
			hash = secrets.PIN
		case domain.LockPassword:
			// A lock with nothing to check against refuses rather than opens.
			if hash = secrets.Password; hash == "" {
				return domain.Profile{}, ErrWrongSecret
			}
		}
		if hash != "" {
			match, _, err := s.hasher.Verify(ctx, hash, secret)
			if err != nil {
				return domain.Profile{}, err
			}
			if !match {
				return domain.Profile{}, ErrWrongSecret
			}
		}
	}
	return profile, s.store.SetSessionProfile(ctx, session.ID, target)
}

// SetPIN sets the profile's PIN, or clears it when pin is empty.
func (s *Service) SetPIN(ctx context.Context, profile uuid.UUID, pin string) error {
	if pin == "" {
		return s.store.SetPINHash(ctx, profile, "")
	}
	if len(pin) < 4 || len(pin) > 6 || strings.Trim(pin, "0123456789") != "" {
		return ErrPINNotDigits
	}
	hash, err := s.hasher.Hash(ctx, pin)
	if err != nil {
		return err
	}
	return s.store.SetPINHash(ctx, profile, hash)
}

// ChangePassword sets the session's profile a new password, if current is its password now, and
// signs out the profile's other devices.
func (s *Service) ChangePassword(ctx context.Context, session domain.Session, current, password string) error {
	_, secrets, err := s.store.ProfileSecrets(ctx, session.Profile.ID)
	if err != nil {
		return err
	}
	if secrets.Password == "" {
		return ErrNoPassword
	}
	match, _, err := s.hasher.Verify(ctx, secrets.Password, current)
	if err != nil {
		return err
	}
	if !match {
		return ErrWrongSecret
	}
	hash, err := HashPassword(ctx, password)
	if err != nil {
		return err
	}
	return s.store.ChangePassword(ctx, session.Profile.ID, hash, session.ID)
}
