package auth

import (
	"context"
	"errors"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
)

var ErrDeviceNotFound = errors.New("no such signed-in device")

// scope is whose devices a session may see and sign out: an admin's, every device; anyone else's,
// those on their own profile and, for a manager, on the users it keeps.
func scope(s domain.Session) *uuid.UUID {
	switch s.Profile.Role {
	case domain.RoleAdmin:
		return nil
	case domain.RoleManager, domain.RoleUser:
	}
	return &s.Profile.ID
}

func (s *Service) Devices(ctx context.Context, session domain.Session) ([]store.DeviceListing, error) {
	return s.store.Devices(ctx, scope(session))
}

// SignOutDevice signs a device out, answering the profile it was signed in to.
func (s *Service) SignOutDevice(ctx context.Context, session domain.Session, device uuid.UUID) (uuid.UUID, error) {
	profile, err := s.store.DeleteDevice(ctx, device, scope(session))
	if errors.Is(err, store.ErrNotFound) {
		return uuid.UUID{}, ErrDeviceNotFound
	}
	return profile, err
}
