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
