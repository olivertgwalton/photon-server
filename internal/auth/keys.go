package auth

import (
	"context"
	"errors"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
)

var ErrKeyNotFound = errors.New("no such API key")

// CreateKey makes an API key for a script or another server, as Jellyfin's dashboard does. It acts
// as the admin who made it, with whatever role that profile has when it is used. Its token is
// answered once: only its hash is kept.
func (s *Service) CreateKey(ctx context.Context, creator domain.Session, name string) (uuid.UUID, string, error) {
	token, tokenHash := newToken()
	id, err := s.store.CreateSession(ctx, store.NewSession{
		Kind: domain.SessionKey, ProfileID: creator.Profile.ID, TokenHash: tokenHash, DeviceName: name,
	})
	return id, token, err
}

func (s *Service) Keys(ctx context.Context) ([]store.KeyListing, error) {
	return s.store.Keys(ctx)
}

func (s *Service) RevokeKey(ctx context.Context, id uuid.UUID) error {
	found, err := s.store.DeleteKey(ctx, id)
	if err == nil && !found {
		return ErrKeyNotFound
	}
	return err
}
