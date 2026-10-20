package requests

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/provider"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// finds is one find of a library that searches TMDB, described or not.
type finds struct {
	described bool
	kept      *domain.Metadata
}

func (f *finds) Askable(_ context.Context, _, id uuid.UUID) (store.Askable, error) {
	return store.Askable{ID: id, Kind: domain.ItemMovie, Value: "1396", Described: f.described}, nil
}

func (*finds) Library(context.Context, uuid.UUID) (domain.Library, error) {
	return domain.Library{DiscoverSource: domain.SourceTMDB}, nil
}

func (f *finds) KeepDescription(_ context.Context, _ uuid.UUID, m domain.Metadata) error {
	f.kept, f.described = &m, true
	return nil
}

func (f *finds) RequestablePage(_ context.Context, _, id uuid.UUID) (store.TitlePage, error) {
	page := store.TitlePage{ID: id, Title: "Found"}
	if f.kept != nil {
		page.Tagline = f.kept.Tagline
	}
	return page, nil
}

// silent is a TMDB that answers nothing.
type silent struct{ tmdb }

func (silent) Describe(context.Context, domain.Locale, domain.ItemKind, string, domain.SeasonRequest) (domain.Metadata, map[int]domain.SeasonMetadata, error) {
	return domain.Metadata{}, nil, provider.ErrUnavailable
}

// tagged describes every title with a tagline.
type tagged struct{ tmdb }

func (tagged) Describe(context.Context, domain.Locale, domain.ItemKind, string, domain.SeasonRequest) (domain.Metadata, map[int]domain.SeasonMetadata, error) {
	return domain.Metadata{Tagline: "Remember my name."}, nil, nil
}

// A find is described by its library's search the first time its page is opened, and kept; a
// provider that cannot say leaves the page what the search said.
func TestAFindIsDescribedOnceAsItsPageOpens(t *testing.T) {
	log := slog.New(slog.DiscardHandler)
	server := func() domain.Locale { return domain.Locale{} }
	f := &finds{}
	pages := NewPages(f, registry{domain.SourceTMDB: tagged{}}, server, log)
	page, err := pages.Page(t.Context(), uuid.NewV7(), uuid.NewV7())
	if err != nil || page.Tagline != "Remember my name." || f.kept == nil {
		t.Fatalf("first opened: %+v, %v; want it described and kept", page, err)
	}
	f.kept.Tagline = "kept"
	if page, err := pages.Page(t.Context(), uuid.NewV7(), uuid.NewV7()); err != nil || page.Tagline != "kept" {
		t.Errorf("opened again: %+v, %v; want what was kept, not described again", page, err)
	}

	unsaid := &finds{}
	page, err = NewPages(unsaid, registry{domain.SourceTMDB: silent{}}, server, log).Page(t.Context(), uuid.NewV7(), uuid.NewV7())
	if err != nil || page.Title != "Found" || unsaid.kept != nil {
		t.Errorf("TMDB down: %+v, %v; want the search's page, nothing kept", page, err)
	}
	if errors.Is(err, provider.ErrUnavailable) {
		t.Error("a provider that is down refused the page")
	}
}
