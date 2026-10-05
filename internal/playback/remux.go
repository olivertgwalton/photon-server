package playback

import (
	"context"
	"os"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/hls"
	"github.com/olivertgwalton/photon-server/internal/store"
)

type partStore interface {
	PartFile(ctx context.Context, part uuid.UUID) (root, rel string, err error)
	Keyframes(ctx context.Context, part uuid.UUID) ([]int64, bool, error)
	SaveKeyframes(ctx context.Context, part uuid.UUID, ptsMS []int64) error
}

type keyframer interface {
	Keyframes(ctx context.Context, f *os.File) ([]int64, error)
}

type remuxer interface {
	Open(playback uuid.UUID, sources []hls.Source) error
	Close(playback uuid.UUID)
}

// Remuxes opens a playback's copy as HLS: each of its files with its keyframes, which are indexed
// in the background as a library is scanned and here, before playing, for a file not reached yet.
type Remuxes struct {
	parts  partStore
	frames keyframer
	hls    remuxer
}

func NewRemuxes(parts partStore, frames keyframer, h remuxer) *Remuxes {
	return &Remuxes{parts: parts, frames: frames, hls: h}
}

// Open starts a playback's remux of the parts of a copy, carrying its video and audio as decided.
func (r *Remuxes) Open(ctx context.Context, playback uuid.UUID, parts []store.PlayPart, video domain.VideoPlan, audio *domain.AudioPlan) error {
	// The remux opens its files long after this request has been answered.
	opening := context.WithoutCancel(ctx)
	sources := make([]hls.Source, len(parts))
	for i, p := range parts {
		open := func() (*os.File, error) { return r.open(opening, p.ID) }
		pts, ok, err := r.parts.Keyframes(ctx, p.ID)
		if err != nil {
			return err
		}
		if !ok {
			if pts, err = r.index(ctx, p.ID, open); err != nil {
				return err
			}
		}
		keyframes := make([]time.Duration, len(pts))
		for k, ms := range pts {
			keyframes[k] = time.Duration(ms) * time.Millisecond
		}
		sources[i] = hls.Source{
			Open: open, Video: video, Audio: audio,
			Part: hls.Part{Duration: time.Duration(p.DurationMS) * time.Millisecond, Keyframes: keyframes},
		}
	}
	return r.hls.Open(playback, sources)
}

func (r *Remuxes) Close(playback uuid.UUID) { r.hls.Close(playback) }

func (r *Remuxes) index(ctx context.Context, part uuid.UUID, open func() (*os.File, error)) ([]int64, error) {
	f, err := open()
	if err != nil {
		return nil, err
	}
	defer f.Close()
	pts, err := r.frames.Keyframes(ctx, f)
	if err != nil {
		return nil, err
	}
	return pts, r.parts.SaveKeyframes(ctx, part, pts)
}

// open opens a part's file through its library's root, so a path can never leave the library.
func (r *Remuxes) open(ctx context.Context, part uuid.UUID) (*os.File, error) {
	root, rel, err := r.parts.PartFile(ctx, part)
	if err != nil {
		return nil, err
	}
	lib, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer lib.Close()
	return lib.Open(rel)
}
