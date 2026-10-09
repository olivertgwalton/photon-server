package httpapi

import (
	"context"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// noCopies is a server with no remote library: every title's copies are the scanner's.
type noCopies struct{}

func (noCopies) Ensure(context.Context, uuid.UUID) error { return nil }

// noDiscoveries is a server whose remote libraries search nothing.
type noDiscoveries struct{}

func (noDiscoveries) Find(context.Context, uuid.UUID, string, []domain.ItemKind) ([]store.Discovery, error) {
	return nil, nil
}
