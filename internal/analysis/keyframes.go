package analysis

import (
	"context"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/jobs"
	"github.com/olivertgwalton/photon-server/internal/media"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// Keyframes indexes a part's keyframes, which a remux cuts its segments at.
func Keyframes(st *store.Store, tools media.Tools) jobs.Handler {
	return func(ctx context.Context, part uuid.UUID) error {
		f, err := openPart(ctx, st, part)
		if err != nil {
			return err
		}
		defer f.Close()
		pts, err := tools.Keyframes(ctx, f, domain.KeyframesFull)
		if err != nil {
			return err
		}
		return st.SaveKeyframes(ctx, part, pts)
	}
}
