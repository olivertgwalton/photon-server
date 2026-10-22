package jellyfin

import (
	"context"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// noRemoteVersions is a server with no remote library: every title's versions are the scanner's.
type noRemoteVersions struct{}

func (noRemoteVersions) Ensure(context.Context, uuid.UUID) error { return nil }

// noDiscoveries is a server whose remote libraries search nothing.
type noDiscoveries struct{}

func (noDiscoveries) Find(context.Context, uuid.UUID, domain.LibraryMedia, string, []domain.ItemKind) ([]store.Discovery, error) {
	return nil, nil
}
