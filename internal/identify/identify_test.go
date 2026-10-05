//go:build integration

package identify

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/provider"
	"github.com/olivertgwalton/photon-server/internal/store"
	"github.com/olivertgwalton/photon-server/internal/store/storetest"
)

// films knows Jaws, by any title, and gives its IMDb id.
type films struct{}

func (films) Info() provider.Info {
	return provider.Info{ID: domain.SourceTMDB, Name: "Films", Kinds: []domain.ItemKind{domain.ItemMovie}}
}

func (films) Match(_ context.Context, _ domain.ItemKind, h provider.Hints) (string, error) {
	return "578", nil
}

func (films) Describe(context.Context, domain.ItemKind, string, domain.SeasonRequest) (domain.Metadata, map[int]domain.SeasonMetadata, error) {
	return domain.Metadata{Title: "Jaws", Overview: "A shark.", IDs: map[domain.Provider]string{domain.ProviderIMDb: "tt0073195"}}, nil, nil
}

// critics rates a title found by the IMDb id an earlier provider gave, or fails as one past its
// allowance does.
type critics struct{ fail bool }

func (critics) Info() provider.Info {
	return provider.Info{ID: domain.SourceMDBList, Name: "Critics", Kinds: []domain.ItemKind{domain.ItemMovie}}
}

func (c critics) Ratings(_ context.Context, _ domain.ItemKind, ids map[domain.Provider]string) ([]domain.Rating, error) {
	if c.fail {
		return nil, errors.New("daily limit exceeded")
	}
	if ids[domain.ProviderIMDb] != "tt0073195" {
		return nil, nil
	}
	return []domain.Rating{{Site: domain.SiteIMDb, Score: 81}}, nil
}

func TestEachProviderTheLibraryTakesIsAsked(t *testing.T) {
	log := slog.New(slog.DiscardHandler)
	url := storetest.FreshDatabase(t)
	if err := store.Migrate(t.Context(), url, log); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(t.Context(), url, log)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := t.Context()
	lib, err := st.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetLibrary(ctx, lib.ID, store.LibraryChange{Sources: []domain.FieldSource{domain.SourceTMDB, domain.SourceMDBList}}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveFolder(ctx, lib.ID, "jaws", []byte("v1"), []store.Film{{Title: "jaws", Folder: "jaws"}}, nil); err != nil {
		t.Fatal(err)
	}
	cards, _, err := st.Wall(ctx, lib.ID, store.WallPage{Sort: domain.SortTitle, Limit: 1})
	if err != nil || len(cards) != 1 {
		t.Fatal(cards, err)
	}
	id := cards[0].ID

	var told []domain.Event
	raise := func(_ context.Context, e domain.Event) { told = append(told, e) }
	// A failing rater does not fail the job: the match stands.
	if err := Handler(st, provider.NewRegistry(nil, films{}, critics{fail: true}), raise, log)(ctx, id); err != nil {
		t.Fatalf("with the rater failing: %v", err)
	}
	if err := Handler(st, provider.NewRegistry(nil, films{}, critics{}), raise, log)(ctx, id); err != nil {
		t.Fatal(err)
	}
	if len(told) != 2 || told[1].Kind != domain.EventTitleUpdated || told[1].Item != id {
		t.Errorf("told %+v; want title.updated for Jaws after each match", told)
	}
	page, err := st.Title(ctx, uuid.UUID{}, id)
	if err != nil {
		t.Fatal(err)
	}
	if page.Title != "Jaws" || page.Overview != "A shark." || len(page.Ratings) != 1 || page.Ratings[0].Score != 81 {
		t.Errorf("page = %q %q %v; want TMDB's description and the rating found by the IMDb id it gave", page.Title, page.Overview, page.Ratings)
	}
}
