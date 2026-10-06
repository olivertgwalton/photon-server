//go:build integration

package scan

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// themes answers the files a title's page plays, in order.
func (f *fixture) themes(id uuid.UUID) []string {
	f.t.Helper()
	page, err := f.st.Title(f.t.Context(), uuid.UUID{}, id)
	if err != nil {
		f.t.Fatal(err)
	}
	var out []string
	for _, t := range page.Themes {
		file, err := f.st.Theme(f.t.Context(), t)
		if err != nil {
			f.t.Fatal(err)
		}
		out = append(out, file.Path)
	}
	return out
}

func TestThemeTunesBesideAFilmAreItsUntilTheyGo(t *testing.T) {
	f := newFixture(t, domain.LibraryMovies)
	f.put("Heat (1995)/Heat (1995).mkv", "heat")
	f.put("Heat (1995)/theme.mp3", "t")
	f.put("Heat (1995)/theme-music/Second.ogg", "s")
	f.put("Heat (1995)/theme-music/notes.txt", "n")
	f.put("Alien (1979).mkv", "alien")
	f.put("theme.mp3", "root")
	f.scan()
	if n := f.count("SELECT count(*) FROM items"); n != 2 {
		t.Errorf("%d titles, want Heat and Alien alone", n)
	}
	heat := f.id(`SELECT id::text FROM items WHERE title = 'Heat'`)
	want := []string{"Heat (1995)/theme.mp3", "Heat (1995)/theme-music/Second.ogg"}
	if got := f.themes(heat); !slices.Equal(got, want) {
		t.Errorf("Heat's themes = %q, want %q", got, want)
	}
	if got := f.themes(f.id(`SELECT id::text FROM items WHERE title = 'Alien'`)); len(got) != 0 {
		t.Errorf("a film at the library's root has themes %q, want none", got)
	}

	if err := os.Remove(filepath.Join(f.root, "Heat (1995)", "theme.mp3")); err != nil {
		t.Fatal(err)
	}
	f.scan()
	if got := f.themes(heat); !slices.Equal(got, want[1:]) {
		t.Errorf("after theme.mp3 went, themes = %q, want %q", got, want[1:])
	}
}

func TestAShowsThemeIsItsSeasonsAndEpisodes(t *testing.T) {
	f := newFixture(t, domain.LibraryShows)
	f.put("The Wire/Season 1/The Wire S01E01.mkv", "e1")
	f.put("The Wire/Theme.FLAC", "t")
	f.scan()
	want := []string{"The Wire/Theme.FLAC"}
	for _, kind := range []string{"show", "season", "episode"} {
		if got := f.themes(f.id(`SELECT id::text FROM items WHERE kind = '` + kind + `'`)); !slices.Equal(got, want) {
			t.Errorf("the %s's themes = %q, want the show's %q", kind, got, want)
		}
	}
}
