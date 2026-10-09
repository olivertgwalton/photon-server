package analysis

import (
	"context"
	"errors"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/jobs"
	"github.com/olivertgwalton/photon-server/internal/media"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// Keyframes reads a part's keyframes, which a remux cuts its segments at, from its container's own
// index, as its library asks: a few reads, made as the part is added. A file with no index is known
// to have none under KeyframesIndex; under KeyframesFull it is queued to be walked through in the
// maintenance window, as Jellyfin reads a Matroska file's index on demand and walks files only in
// a scheduled task of its own.
func Keyframes(st *store.Store) jobs.Handler {
	return func(ctx context.Context, part uuid.UUID) error {
		known, err := st.Keyframes(ctx, part)
		if err != nil || known.Mode == domain.KeyframesOff {
			return err
		}
		in, err := openPart(ctx, st, part)
		if err != nil {
			return err
		}
		defer in.Close()
		pts, err := media.IndexedKeyframes(in)
		switch {
		case err == nil:
			return st.SaveKeyframes(ctx, part, pts)
		case !errors.Is(err, media.ErrNoIndex):
			return err
		}
		switch known.Mode {
		case domain.KeyframesFull:
			return st.QueueKeyframeWalk(ctx, part)
		case domain.KeyframesIndex, domain.KeyframesOff:
		}
		return st.SaveKeyframes(ctx, part, nil)
	}
}

// WalkKeyframes walks a part with no index through for its keyframes. One whose library no longer
// finds them in full, or whose keyframes are known since, is passed over.
func WalkKeyframes(st *store.Store, tools media.Tools) jobs.Handler {
	return func(ctx context.Context, part uuid.UUID) error {
		known, err := st.Keyframes(ctx, part)
		if err != nil || known.Mode != domain.KeyframesFull || known.PtsMS != nil {
			return err
		}
		in, err := openPart(ctx, st, part)
		if err != nil {
			return err
		}
		defer in.Close()
		pts, err := tools.WalkKeyframes(ctx, in)
		if err != nil {
			return err
		}
		return st.SaveKeyframes(ctx, part, pts)
	}
}
