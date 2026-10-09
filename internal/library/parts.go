package library

import (
	"context"
	"fmt"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/media"
)

// Places finds where a part's bytes are: the root of its library and the path inside it.
type Places interface {
	PartFile(ctx context.Context, part uuid.UUID) (root, rel string, err error)
}

// Parts opens the parts of the copies the scanner recorded, for a tool to read or a player to
// fetch.
type Parts struct {
	Places Places
}

func (p Parts) Open(ctx context.Context, part uuid.UUID) (media.Input, error) {
	root, rel, err := p.Places.PartFile(ctx, part)
	if err != nil {
		return media.Input{}, err
	}
	in, err := OpenMedia(root, rel)
	if err != nil {
		return media.Input{}, fmt.Errorf("part %s: %w", part, err)
	}
	return in, nil
}
