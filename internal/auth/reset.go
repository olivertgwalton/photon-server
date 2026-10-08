package auth

import (
	"context"
	"errors"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/store"
)

// ResetTTL is how long a password reset's code may be used, as Jellyfin's reset PIN lasts.
const ResetTTL = 30 * time.Minute

var ErrResetNotFound = errors.New("no password reset is waiting for that code")

// StartReset makes a reset of the password of the profile named name, answering its code, for the
// server's log: whoever can read that is the server's operator. There is no code where no profile
// has the name, and the caller answers the same either way.
func (s *Service) StartReset(ctx context.Context, name string) (code string, err error) {
	profile, _, err := s.store.ProfileByName(ctx, name)
	if errors.Is(err, store.ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	for range 5 {
		code, err := CodeLetters.newCode()
		if err != nil {
			return "", err
		}
		started, err := s.kv.StartReset(ctx, hashToken(code), profile.ID, ResetTTL)
		if err != nil || started {
			return CodeLetters.shown(code), err
		}
	}
	return "", errNoFreeCode
}

// RedeemReset sets a new password on the profile a reset's code is for, and signs out its every
// device, as a password changed for fear of who knows it must. The code is used once, and not by a
// password refused for being too short.
func (s *Service) RedeemReset(ctx context.Context, code, password string) (uuid.UUID, error) {
	hash, err := HashPassword(ctx, password)
	if err != nil {
		return uuid.UUID{}, err
	}
	profile, ok, err := s.kv.TakeReset(ctx, hashToken(typedCode(code)))
	if err != nil {
		return uuid.UUID{}, err
	}
	if !ok {
		return uuid.UUID{}, ErrResetNotFound
	}
	return profile, s.store.ChangePassword(ctx, profile, hash, uuid.UUID{})
}
