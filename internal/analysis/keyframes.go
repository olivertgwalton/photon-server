package analysis

import (
	"context"
	"os"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/jobs"
	"github.com/olivertgwalton/photon-server/internal/media"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// Keyframes indexes a part's keyframes, which a remux cuts its segments at.
func Keyframes(st *store.Store, tools media.Tools) jobs.Handler {
	return func(ctx context.Context, part uuid.UUID) error {
		root, rel, err := st.PartFile(ctx, part)
		if err != nil {
			return err
		}
		r, err := os.OpenRoot(root)
		if err != nil {
			return err
		}
		defer r.Close()
		f, err := r.Open(rel)
		if err != nil {
			return err
		}
		defer f.Close()
		pts, err := tools.Keyframes(ctx, f)
		if err != nil {
			return err
		}
		return st.SaveKeyframes(ctx, part, pts)
	}
}
