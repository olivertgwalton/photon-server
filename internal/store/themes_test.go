//go:build integration

package store

import (
	"slices"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// A library keeps to its titles' own files until it takes ThemerrDB's. Then a title TMDB knows is
// asked for its theme as it is matched, and again on every match after, so a changed link is
// found; its page plays its own file over ThemerrDB's, as its library allows.
func TestAThemeComesFromItsFolderElseThemerrDB(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "Shows", domain.LibraryShows, "/srv/shows")
	if err != nil {
		t.Fatal(err)
	}
	if lib.Themes != domain.ThemesLocal {
		t.Errorf("a new library takes themes from %q, want its files alone", lib.Themes)
	}
	wire := Show{Title: "The Wire", Folder: "The Wire", IDs: map[domain.Provider]string{domain.ProviderTMDB: "1438"}, NFO: &domain.Metadata{}}
	if _, err := s.SaveShowFolder(ctx, lib.ID, "The Wire", []byte("v1"), wire, nil, nil); err != nil {
		t.Fatal(err)
	}
	var show uuid.UUID
	if err := s.pool.QueryRow(ctx, `SELECT id FROM items WHERE kind = 'show'`).Scan(&show); err != nil {
		t.Fatal(err)
	}
	asked := func() int64 {
		t.Helper()
		tag, err := s.pool.Exec(ctx, `DELETE FROM jobs WHERE kind = 'theme'`)
		if err != nil {
			t.Fatal(err)
		}
		return tag.RowsAffected()
	}
	themes := func() []uuid.UUID {
		t.Helper()
		page, err := s.Title(ctx, uuid.UUID{}, show)
		if err != nil {
			t.Fatal(err)
		}
		return page.Themes
	}

	if err := s.Identified(ctx, show); err != nil {
		t.Fatal(err)
	}
	if n := asked(); n != 0 {
		t.Errorf("matched in a library of local themes: %d fetches queued, want none", n)
	}
	if err := s.SetLibrary(ctx, lib.ID, LibraryChange{Themes: domain.ThemesThemerr}); err != nil {
		t.Fatal(err)
	}
	if n := asked(); n != 1 {
		t.Errorf("the library taking ThemerrDB's: %d fetches queued, want 1", n)
	}
	if subject, ok, err := s.ThemeSubject(ctx, show); err != nil || !ok || subject != (ThemeSubject{Kind: domain.ItemShow, TMDB: "1438"}) {
		t.Errorf("theme subject = %+v, %v, %v; want The Wire by TMDB's id", subject, ok, err)
	}
	link := "https://www.youtube.com/watch?v=wire"
	fetched := uuid.NewV7()
	if err := s.SaveFetchedTheme(ctx, show, fetched, link); err != nil {
		t.Fatal(err)
	}
	if got := themes(); !slices.Equal(got, []uuid.UUID{fetched}) {
		t.Errorf("themes = %v, want ThemerrDB's %v", got, fetched)
	}
	if f, err := s.Theme(ctx, fetched); err != nil || f != (ThemeFile{Source: domain.ThemeFromThemerr}) {
		t.Errorf("fetched theme = %+v, %v; want it kept in the cache", f, err)
	}
	if err := s.Identified(ctx, show); err != nil {
		t.Fatal(err)
	}
	if n := asked(); n != 1 {
		t.Errorf("matched again: %d fetches queued, want 1, to see whether its link changed", n)
	}
	if subject, _, _ := s.ThemeSubject(ctx, show); subject.Theme != fetched || subject.URL != link {
		t.Errorf("theme subject = %+v, want the theme fetched and its link", subject)
	}
	refetched := uuid.NewV7()
	if err := s.SaveFetchedTheme(ctx, show, refetched, link+"2"); err != nil {
		t.Fatal(err)
	}
	if got := themes(); !slices.Equal(got, []uuid.UUID{refetched}) {
		t.Errorf("fetched from a new link: themes = %v, want %v alone", got, refetched)
	}

	if err := s.SetLibrary(ctx, lib.ID, LibraryChange{Themes: domain.ThemesLocal}); err != nil {
		t.Fatal(err)
	}
	if got := themes(); len(got) != 0 {
		t.Errorf("a library of local themes plays %v, want nothing from ThemerrDB", got)
	}
	if err := s.SetLibrary(ctx, lib.ID, LibraryChange{Themes: domain.ThemesThemerr}); err != nil {
		t.Fatal(err)
	}
	wire.Themes = []string{"The Wire/theme.mp3"}
	if _, err := s.SaveShowFolder(ctx, lib.ID, "The Wire", []byte("v2"), wire, nil, nil); err != nil {
		t.Fatal(err)
	}
	got := themes()
	if len(got) != 1 || got[0] == refetched {
		t.Fatalf("themes = %v, want the show's own file alone", got)
	}
	if f, err := s.Theme(ctx, got[0]); err != nil || f.Path != "The Wire/theme.mp3" || f.Root != "/srv/shows" {
		t.Errorf("own theme = %+v, %v; want The Wire/theme.mp3 under the library", f, err)
	}
	asked()
	if err := s.Identified(ctx, show); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.ThemeSubject(ctx, show); ok || asked() != 0 {
		t.Error("a show with its own theme file is asked of ThemerrDB, want it left be")
	}
	if err := s.SetLibrary(ctx, lib.ID, LibraryChange{Themes: domain.ThemesOff}); err != nil {
		t.Fatal(err)
	}
	if got := themes(); len(got) != 0 {
		t.Errorf("a library of no themes plays %v", got)
	}
}

// A film ThemerrDB lists by IMDb's id alone is asked for too.
func TestAFilmIsAskedByIMDbsID(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	film := Film{
		Title: "Heat", Folder: "Heat", IDs: map[domain.Provider]string{domain.ProviderIMDb: "tt0113277"},
		Copies: []Copy{{ContentKey: []byte("heat"), Parts: []Part{{
			RelPath: "Heat/Heat.mkv", Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{},
		}}}},
	}
	if _, err := s.SaveFolder(ctx, lib.ID, "Heat", []byte("v1"), []Film{film}, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.SetLibrary(ctx, lib.ID, LibraryChange{Themes: domain.ThemesThemerr}); err != nil {
		t.Fatal(err)
	}
	var job uuid.UUID
	if err := s.pool.QueryRow(ctx, `SELECT subject FROM jobs WHERE kind = 'theme'`).Scan(&job); err != nil {
		t.Fatalf("no theme fetch queued for Heat: %v", err)
	}
	if subject, ok, err := s.ThemeSubject(ctx, job); err != nil || !ok || subject != (ThemeSubject{Kind: domain.ItemMovie, IMDb: "tt0113277"}) {
		t.Errorf("theme subject = %+v, %v, %v; want Heat by IMDb's id", subject, ok, err)
	}
}
