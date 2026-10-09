package library

import (
	"context"
	"fmt"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/media"
)

// Places finds where a part's bytes are.
type Places interface {
	PartPlace(ctx context.Context, part uuid.UUID) (domain.Place, error)
}

// Streams opens a copy a provider streams, as its offers say where it is now.
type Streams interface {
	Open(ctx context.Context, p domain.Place) (media.Input, error)
}

// Parts opens the parts of the copies the catalogue holds, for a tool to read or a player to
// fetch: a file under a folder library's root, or a copy a remote library's provider streams.
type Parts struct {
	Places  Places
	Streams Streams
}

func (p Parts) Open(ctx context.Context, part uuid.UUID) (media.Input, error) {
	place, err := p.Places.PartPlace(ctx, part)
	if err != nil {
		return media.Input{}, err
	}
	var in media.Input
	switch place.Media {
	case domain.MediaRemote:
		in, err = p.Streams.Open(ctx, place)
	case domain.MediaFolder:
		in, err = OpenMedia(place.Root, place.Rel)
	}
	if err != nil {
		return media.Input{}, fmt.Errorf("part %s: %w", part, err)
	}
	return in, nil
}
