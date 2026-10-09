package analysis

import (
	"context"
	"errors"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/jobs"
	"github.com/olivertgwalton/photon-server/internal/provider"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// Segments asks the providers that time intros and credits about a film's or an episode's parts
// not yet asked about, each once. A title gone, or a server with no such provider, has nothing
// asked.
func Segments(st *store.Store, providers *provider.Registry) jobs.Handler {
	return func(ctx context.Context, item uuid.UUID) error {
		ask, err := st.SegmentsAsk(ctx, item)
		if errors.Is(err, store.ErrNotFound) || err == nil && len(ask.Read) == 0 {
			return nil
		}
		if err != nil {
			return err
		}
		found := map[uuid.UUID][]domain.Marker{}
		for part, length := range ask.Parts {
			q := ask.Title
			q.Duration = length
			markers, err := providers.Segments(ctx, q)
			if errors.Is(err, provider.ErrNoSegmenter) {
				return nil
			}
			if err != nil {
				return err
			}
			found[part] = markers
		}
		return st.SaveFoundMarkers(ctx, domain.MarkerByProvider, ask.Read, found)
	}
}
