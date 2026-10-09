package analysis

import (
	"context"
	"errors"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/jobs"
	"github.com/olivertgwalton/photon-server/internal/media"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// opener opens a part for a tool to read.
type opener interface {
	Open(ctx context.Context, part uuid.UUID) (media.Input, error)
}

// Probe reads a part's file again for its streams, chapters and length, as an admin's Analyse
// asks, and saves them over what it was read to hold when it was added. A part gone since is
// passed over.
func Probe(st *store.Store, parts opener, tools media.Tools) jobs.Handler {
	return func(ctx context.Context, part uuid.UUID) error {
		in, err := parts.Open(ctx, part)
		if errors.Is(err, store.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		defer in.Close()
		facts, err := tools.Probe(ctx, in)
		if err != nil {
			return err
		}
		return st.SaveProbe(ctx, part, facts)
	}
}
