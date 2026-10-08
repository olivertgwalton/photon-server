//go:build integration

package identify

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/artwork"
	"github.com/olivertgwalton/photon-server/internal/blob"
	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/provider"
	"github.com/olivertgwalton/photon-server/internal/store"
	"github.com/olivertgwalton/photon-server/internal/store/storetest"
)

// films knows Jaws, by any title, and gives its IMDb id and poster.
type films struct{ poster string }

func (films) Info() provider.Info {
	return provider.Info{ID: domain.SourceTMDB, Name: "Films", Kinds: []domain.ItemKind{domain.ItemMovie}}
}

func (films) Match(_ context.Context, _ domain.Locale, _ domain.ItemKind, h provider.Hints) (string, error) {
	return "578", nil
}

func (f films) Describe(context.Context, domain.Locale, domain.ItemKind, string, domain.SeasonRequest) (domain.Metadata, map[int]domain.SeasonMetadata, error) {
	return domain.Metadata{
		Title: "Jaws", Overview: "A shark.", IDs: map[domain.Provider]string{domain.ProviderIMDb: "tt0073195"},
		Artwork: []domain.Artwork{{Kind: domain.ArtworkPoster, URL: f.poster}},
	}, nil, nil
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
	if err := st.SetLibrary(ctx, lib.ID, store.LibraryChange{Sources: []domain.KindSources{{
		Kind:     domain.ItemMovie,
		Metadata: []domain.RankedSource{{Source: domain.SourceTMDB, Enabled: true}, {Source: domain.SourceMDBList, Enabled: true}},
	}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveFolder(ctx, lib.ID, "jaws", []byte("v1"), []store.Film{{Title: "jaws", Folder: "jaws"}}, nil); err != nil {
		t.Fatal(err)
	}
	cards, _, err := st.Wall(ctx, []uuid.UUID{lib.ID}, store.WallPage{Sort: domain.SortTitle, Limit: 1})
	if err != nil || len(cards) != 1 {
		t.Fatal(cards, err)
	}
	id := cards[0].ID

	var poster bytes.Buffer
	if err := png.Encode(&poster, image.NewGray(image.Rect(0, 0, 4, 6))); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		if _, err := w.Write(poster.Bytes()); err != nil {
			t.Error(err)
		}
	}))
	defer srv.Close()
	dir := t.TempDir()
	blobs, err := blob.OpenDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer blobs.Close()
	cache := artwork.New(blobs, st.SetBlurhash)
	jaws := films{poster: srv.URL + "/jaws.png"}

	var told []domain.Event
	raise := func(_ context.Context, e domain.Event) {
		told = append(told, e)
		page, err := st.Title(ctx, uuid.UUID{}, id)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(dir, page.Artwork[domain.ArtworkPoster][0].String())); err != nil {
			t.Errorf("told the title changed before its poster was fetched: %v", err)
		}
	}
	// A failing rater does not fail the job: the match stands.
	if err := Handler(st, provider.NewRegistry(nil, jaws, critics{fail: true}), cache, domain.LocaleOf("en-GB"), raise, log)(ctx, id); err != nil {
		t.Fatalf("with the rater failing: %v", err)
	}
	if err := Handler(st, provider.NewRegistry(nil, jaws, critics{}), cache, domain.LocaleOf("en-GB"), raise, log)(ctx, id); err != nil {
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

// pictures is a picture cache of the test's own.
func pictures(t *testing.T) *artwork.Cache {
	blobs, err := blob.OpenDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = blobs.Close() })
	c := artwork.New(blobs, func(context.Context, uuid.UUID, string) error { return nil })
	return c
}

// localFilms describes Jaws in whatever it is asked in, with India's adult certificate.
type localFilms struct{ asked *domain.Locale }

func (localFilms) Info() provider.Info {
	return provider.Info{ID: domain.SourceTMDB, Name: "Films", Kinds: []domain.ItemKind{domain.ItemMovie}}
}

func (localFilms) Match(context.Context, domain.Locale, domain.ItemKind, provider.Hints) (string, error) {
	return "578", nil
}

func (f localFilms) Describe(_ context.Context, loc domain.Locale, _ domain.ItemKind, _ string, _ domain.SeasonRequest) (domain.Metadata, map[int]domain.SeasonMetadata, error) {
	*f.asked = loc
	return domain.Metadata{Title: "Der weiße Hai", OriginalTitle: "Jaws", Certificate: "A"}, nil, nil
}

func TestATitleIsDescribedInItsLibrarysLocale(t *testing.T) {
	url := storetest.FreshDatabase(t)
	log := slog.New(slog.DiscardHandler)
	if err := store.Migrate(t.Context(), url, log); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(t.Context(), url, log)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := t.Context()
	lib, err := st.AddLibrary(ctx, "Filme", domain.LibraryMovies, "/srv/filme")
	if err != nil {
		t.Fatal(err)
	}
	german, india := "de-DE", "IN"
	if err := st.SetLibrary(ctx, lib.ID, store.LibraryChange{MetadataLanguage: &german, CertificationCountry: &india}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveFolder(ctx, lib.ID, "jaws", []byte("v1"), []store.Film{{Title: "jaws", Folder: "jaws"}}, nil); err != nil {
		t.Fatal(err)
	}
	cards, _, err := st.Wall(ctx, []uuid.UUID{lib.ID}, store.WallPage{Sort: domain.SortTitle, Limit: 1})
	if err != nil || len(cards) != 1 {
		t.Fatal(cards, err)
	}
	var asked domain.Locale
	if err := Handler(st, provider.NewRegistry(nil, localFilms{&asked}), pictures(t), domain.LocaleOf("en-GB"), func(context.Context, domain.Event) {}, log)(ctx, cards[0].ID); err != nil {
		t.Fatal(err)
	}
	if asked != (domain.Locale{Language: "de-DE", Country: "IN", Artwork: domain.ArtworkLocalized}) {
		t.Errorf("the provider was asked in %+v, want the library's de-DE and India", asked)
	}
	page, err := st.Title(ctx, uuid.UUID{}, cards[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	// India's A is for adults; the server reads Britain's, where it would be no certificate at all.
	if page.Title != "Der weiße Hai" || page.Certificate != "IN:A" {
		t.Errorf("described as %q, %q; want the German title and India's certificate, written as India's", page.Title, page.Certificate)
	}
}

func TestALibraryGivingOriginalTitlesNamesATitleAsItWasFirstNamed(t *testing.T) {
	url := storetest.FreshDatabase(t)
	log := slog.New(slog.DiscardHandler)
	if err := store.Migrate(t.Context(), url, log); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(t.Context(), url, log)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := t.Context()
	lib, err := st.AddLibrary(ctx, "Filme", domain.LibraryMovies, "/srv/filme")
	if err != nil {
		t.Fatal(err)
	}
	german := "de-DE"
	if err := st.SetLibrary(ctx, lib.ID, store.LibraryChange{MetadataLanguage: &german, TitleLanguage: domain.TitlesOriginal}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveFolder(ctx, lib.ID, "jaws", []byte("v1"), []store.Film{{Title: "jaws", Folder: "jaws"}}, nil); err != nil {
		t.Fatal(err)
	}
	cards, _, err := st.Wall(ctx, []uuid.UUID{lib.ID}, store.WallPage{Sort: domain.SortTitle, Limit: 1})
	if err != nil || len(cards) != 1 {
		t.Fatal(cards, err)
	}
	var asked domain.Locale
	if err := Handler(st, provider.NewRegistry(nil, localFilms{&asked}), pictures(t), domain.LocaleOf("en-GB"), func(context.Context, domain.Event) {}, log)(ctx, cards[0].ID); err != nil {
		t.Fatal(err)
	}
	page, err := st.Title(ctx, uuid.UUID{}, cards[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if page.Title != "Jaws" {
		t.Errorf("titled %q, want Jaws, its original title, though its write-up is asked in German", page.Title)
	}
}
