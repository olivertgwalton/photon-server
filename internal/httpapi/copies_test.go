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

func (noDiscoveries) Find(context.Context, uuid.UUID, domain.LibraryMedia, string, []domain.ItemKind) ([]store.Discovery, error) {
	return nil, nil
}

// findsOfEach is a server with a remote library and a library of files that each search: the
// remote one finds Inception, the other Heat.
type findsOfEach struct{}

func (findsOfEach) Find(_ context.Context, _ uuid.UUID, media domain.LibraryMedia, _ string, _ []domain.ItemKind) ([]store.Discovery, error) {
	switch media {
	case domain.MediaRemote:
		return []store.Discovery{{ID: uuid.NewV7(), Kind: domain.ItemMovie, Title: "Inception"}}, nil
	case domain.MediaFolder:
		return []store.Discovery{{ID: uuid.NewV7(), Kind: domain.ItemMovie, Title: "Heat"}}, nil
	}
	return nil, nil
}
