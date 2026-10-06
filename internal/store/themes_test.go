//go:build integration

package store

import (
	"slices"
	"testing"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// A show TheTVDB knows is asked for its theme as it is matched, until it has one; its page plays
// its own file over the theme host's, as its library allows.
func TestAShowsThemeComesFromItsFolderElseTheThemeHost(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "Shows", domain.LibraryShows, "/srv/shows")
	if err != nil {
		t.Fatal(err)
	}
	wire := Show{Title: "The Wire", Folder: "The Wire", IDs: map[domain.Provider]string{domain.ProviderTVDB: "79126"}, NFO: &domain.Metadata{}}
	if _, err := s.SaveShowFolder(ctx, lib.ID, "The Wire", []byte("v1"), wire, nil, nil); err != nil {
		t.Fatal(err)
	}
	i, j := s.q.Item, s.q.Job
	row, err := i.WithContext(ctx).Where(i.Kind.Eq(string(domain.ItemShow))).Take()
	if err != nil {
		t.Fatal(err)
	}
	show := uuid.UUID(row.ID)
	asked := func() int64 {
		t.Helper()
		n, err := j.WithContext(ctx).Where(j.Kind.Eq(string(domain.JobTheme))).Count()
		if err != nil {
			t.Fatal(err)
		}
		_, _ = j.WithContext(ctx).Where(j.Kind.Eq(string(domain.JobTheme))).Delete()
		return n
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
	if n := asked(); n != 1 {
		t.Errorf("matched with TheTVDB's id: %d theme fetches queued, want 1", n)
	}
	if tvdb, ok, err := s.ThemeSubject(ctx, show); err != nil || !ok || tvdb != "79126" {
		t.Errorf("theme subject = %q, %v, %v; want The Wire's TheTVDB id", tvdb, ok, err)
	}
	hosted := uuid.NewV7()
	if err := s.SaveFetchedTheme(ctx, show, hosted, "https://tvthemes.plexapp.com/79126.mp3"); err != nil {
		t.Fatal(err)
	}
	if got := themes(); !slices.Equal(got, []uuid.UUID{hosted}) {
		t.Errorf("themes = %v, want the theme host's %v", got, hosted)
	}
	if f, err := s.Theme(ctx, hosted); err != nil || f.URL != "https://tvthemes.plexapp.com/79126.mp3" {
		t.Errorf("hosted theme = %+v, %v; want its address", f, err)
	}
	if err := s.Identified(ctx, show); err != nil {
		t.Fatal(err)
	}
	if n := asked(); n != 0 {
		t.Errorf("matched again with a theme: %d fetches queued, want none", n)
	}

	if err := s.SetLibrary(ctx, lib.ID, LibraryChange{Themes: domain.ThemesLocal}); err != nil {
		t.Fatal(err)
	}
	if got := themes(); len(got) != 0 {
		t.Errorf("a library of local themes plays %v, want nothing from the host", got)
	}
	if err := s.SetLibrary(ctx, lib.ID, LibraryChange{Themes: domain.ThemesAll}); err != nil {
		t.Fatal(err)
	}
	wire.Themes = []string{"The Wire/theme.mp3"}
	if _, err := s.SaveShowFolder(ctx, lib.ID, "The Wire", []byte("v2"), wire, nil, nil); err != nil {
		t.Fatal(err)
	}
	got := themes()
	if len(got) != 1 || got[0] == hosted {
		t.Fatalf("themes = %v, want the show's own file alone", got)
	}
	if f, err := s.Theme(ctx, got[0]); err != nil || f.Path != "The Wire/theme.mp3" || f.Root != "/srv/shows" {
		t.Errorf("own theme = %+v, %v; want The Wire/theme.mp3 under the library", f, err)
	}
	if err := s.SetLibrary(ctx, lib.ID, LibraryChange{Themes: domain.ThemesOff}); err != nil {
		t.Fatal(err)
	}
	if got := themes(); len(got) != 0 {
		t.Errorf("a library of no themes plays %v", got)
	}
}
