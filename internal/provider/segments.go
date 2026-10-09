package provider

import (
	"context"
	"errors"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// Segmenter times a film's or an episode's intro and credits, as a database of them timed by hand
// for each release does.
type Segmenter interface {
	Provider
	Segments(ctx context.Context, q domain.SegmentQuery) ([]domain.Marker, error)
}

// ErrNoSegmenter is a server with no provider that times intros and credits.
var ErrNoSegmenter = errors.New("no provider times intros and credits")

// Segments asks each provider that times intros and credits, in order, for a title's: the first to
// time any is taken. A provider that fails is passed over, and its error answered where none does.
func (r *Registry) Segments(ctx context.Context, q domain.SegmentQuery) ([]domain.Marker, error) {
	all, err := r.All(ctx)
	if err != nil {
		return nil, err
	}
	var errs []error
	asked := false
	for _, p := range all {
		s, ok := As[Segmenter](p, domain.CapabilitySegments)
		if !ok {
			continue
		}
		asked = true
		markers, err := s.Segments(ctx, q)
		if len(markers) > 0 {
			return markers, nil
		}
		errs = append(errs, err)
	}
	if !asked {
		return nil, ErrNoSegmenter
	}
	return nil, errors.Join(errs...)
}
