package remote

import (
	"context"
	"log/slog"
	"slices"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/provider"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// maxFound is as many of what a search finds as are shown of each library: the best few, as a
// provider ranks them.
const maxFound = 10

type discoveries interface {
	Discoverable(ctx context.Context, profile uuid.UUID, media domain.LibraryMedia) ([]store.Discoverable, error)
	SaveDiscoveries(ctx context.Context, lib uuid.UUID, kind domain.ItemKind, provider domain.Provider, found []domain.Candidate) ([]store.Discovery, error)
}

type searchers interface {
	Get(ctx context.Context, id domain.FieldSource) (provider.Provider, bool, error)
}

// Discover finds libraries titles they do not hold yet, as Remux's search finds what its addons
// have: what a library's provider finds by a name. A remote library's are shown as its titles,
// which become its own as they are opened; a library of files' are titles a profile may ask for. A
// provider that fails leaves out what it would have found.
type Discover struct {
	store     discoveries
	providers searchers
	locale    func() domain.Locale
	log       *slog.Logger
}

func NewDiscover(st discoveries, providers searchers, locale func() domain.Locale, log *slog.Logger) *Discover {
	return &Discover{store: st, providers: providers, locale: locale, log: log}
}

// Find answers what the providers of the libraries of a media a profile may see find by text, of
// the kinds asked for, every kind where none is.
func (d *Discover) Find(ctx context.Context, profile uuid.UUID, media domain.LibraryMedia, text string, kinds []domain.ItemKind) ([]store.Discovery, error) {
	libs, err := d.store.Discoverable(ctx, profile, media)
	if err != nil {
		return nil, err
	}
	var out []store.Discovery
	for _, lib := range libs {
		p, ok, err := d.providers.Get(ctx, lib.Source)
		if err != nil {
			return nil, err
		}
		s, searches := provider.As[provider.Searcher](p, domain.CapabilitySearch)
		if !ok || !searches {
			continue
		}
		for _, kind := range lib.Kind.Titles() {
			if len(kinds) > 0 && !slices.Contains(kinds, kind) {
				continue
			}
			found, err := s.Candidates(ctx, d.locale(), kind, text, 0)
			if err != nil {
				d.log.WarnContext(ctx, "a library's search failed", slog.String("provider", string(lib.Source)), slog.Any("err", err))
				continue
			}
			saved, err := d.store.SaveDiscoveries(ctx, lib.ID, kind, domain.Provider(lib.Source), found[:min(len(found), maxFound)])
			if err != nil {
				return nil, err
			}
			out = append(out, saved...)
		}
	}
	return out, nil
}
