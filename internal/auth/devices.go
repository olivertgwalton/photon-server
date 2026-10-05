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
// the devices on their own profile.
func scope(s domain.Session) *uuid.UUID {
	switch s.Profile.Role {
	case domain.RoleAdmin:
		return nil
	case domain.RoleMember, domain.RoleRestricted:
	}
	return &s.Profile.ID
}

func (s *Service) Devices(ctx context.Context, session domain.Session) ([]store.DeviceListing, error) {
	return s.store.Devices(ctx, scope(session))
}

func (s *Service) SignOutDevice(ctx context.Context, session domain.Session, device uuid.UUID) error {
	found, err := s.store.DeleteDevice(ctx, device, scope(session))
	if err == nil && !found {
		return ErrDeviceNotFound
	}
	return err
}
