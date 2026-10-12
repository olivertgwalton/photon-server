package provider

import (
	"context"
	"errors"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// Lister reads a list kept on a provider, its titles in its order, as Kometa's list builders do.
type Lister interface {
	Provider
	List(ctx context.Context, id string) ([]domain.Listed, error)
}

// ListFinder answers the lists a provider keeps that a collection could follow: its best liked,
// or those named like a query.
type ListFinder interface {
	Lister
	Lists(ctx context.Context, query string) ([]domain.KeptList, error)
}

// ErrNoLister is a provider that keeps no lists, or no such provider.
var ErrNoLister = errors.New("the provider keeps no lists")

// List reads a list kept on the provider source, its titles in its order.
func (r *Registry) List(ctx context.Context, source domain.FieldSource, id string) ([]domain.Listed, error) {
	p, ok, err := r.Get(ctx, source)
	if err != nil {
		return nil, err
	}
	lister, isLister := As[Lister](p, domain.CapabilityList)
	if !ok || !isLister {
		return nil, ErrNoLister
	}
	return lister.List(ctx, id)
}

// Lists answers the lists kept on the provider source that a collection could follow.
func (r *Registry) Lists(ctx context.Context, source domain.FieldSource, query string) ([]domain.KeptList, error) {
	p, ok, err := r.Get(ctx, source)
	if err != nil {
		return nil, err
	}
	finder, isFinder := As[ListFinder](p, domain.CapabilityList)
	if !ok || !isFinder {
		return nil, ErrNoLister
	}
	return finder.Lists(ctx, query)
}
