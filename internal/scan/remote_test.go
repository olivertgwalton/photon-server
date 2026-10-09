//go:build integration

package scan

import (
	"context"
	"log/slog"
	"slices"
	"testing"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
)

func heat() domain.Listed {
	return domain.Listed{Kind: domain.ItemMovie, IDs: map[domain.Provider]string{domain.ProviderIMDb: "tt0113277"}, Title: "Heat", Year: 1995}
}

func alien() domain.Listed {
	return domain.Listed{Kind: domain.ItemMovie, IDs: map[domain.Provider]string{domain.ProviderTMDB: "348"}}
}

// remote makes the fixture's library a remote one holding whatever list l holds when it is read.
func (f *fixture) remote(l *lists) {
	f.t.Helper()
	lib, err := f.st.AddRemoteLibrary(f.t.Context(), "Popular", domain.LibraryMovies, store.Remote{ListSource: domain.PluginSource("aio"), ListID: "movie/top", StreamSource: domain.PluginSource("aio")})
	if err != nil {
		f.t.Fatal(err)
	}
	f.lib = lib
	f.scanner = New(f.st, fakeProber{}, listing{l}, slog.New(slog.DiscardHandler))
}

// listing reads a list as it is when it is read.
type listing struct{ l *lists }

func (l listing) List(ctx context.Context, source domain.FieldSource, id string) ([]domain.Listed, error) {
	return l.l.List(ctx, source, id)
}

func (f *fixture) titles() []string {
	f.t.Helper()
	rows, err := f.db.Query(f.t.Context(), `SELECT title FROM items WHERE library_id = $1 ORDER BY title`, f.lib.ID)
	if err != nil {
		f.t.Fatal(err)
	}
	var out []string
	for rows.Next() {
		var title string
		if err := rows.Scan(&title); err != nil {
			f.t.Fatal(err)
		}
		out = append(out, title)
	}
	return out
}

// A remote library holds its list's titles, named as the list names them until they are matched,
// by an id where the list names none. A film it no longer lists goes, unless someone has played
// it; one the list cannot be read does not empty it.
func TestARemoteLibraryHoldsItsListsTitles(t *testing.T) {
	f := newFixture(t, domain.LibraryMovies)
	list := lists{heat(), alien(), {Kind: domain.ItemShow, IDs: map[domain.Provider]string{domain.ProviderIMDb: "tt0903747"}}}
	f.remote(&list)
	f.scan()
	if got := f.titles(); !slices.Equal(got, []string{"348", "Heat"}) {
		t.Errorf("titles %q, want Heat and Alien by its TMDB id, and not the show", got)
	}
	if n := f.count(`SELECT count(*) FROM jobs WHERE kind = 'identify'`); n != 2 {
		t.Errorf("%d titles to match, want both", n)
	}
	f.scan()
	if got := f.titles(); len(got) != 2 {
		t.Errorf("read again, titles %q, want the same two", got)
	}

	ada, err := f.st.AddProfile(t.Context(), "Ada", domain.RoleAdmin, "hash", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.st.MarkWatched(t.Context(), ada.ID, f.id(`SELECT id FROM items WHERE title = 'Heat'`), nil); err != nil {
		t.Fatal(err)
	}
	list = lists{}
	f.scan()
	if got := f.titles(); !slices.Equal(got, []string{"Heat"}) {
		t.Errorf("listing neither, titles %q, want Heat, which Ada watched", got)
	}

	list = nil
	if _, err := f.scanner.Scan(t.Context(), f.lib, nil, func(domain.ScanProgress) {}, func(store.Changed) {}); err == nil {
		t.Error("a list that cannot be read scanned, want it to fail")
	}
	if got := f.titles(); !slices.Equal(got, []string{"Heat"}) {
		t.Errorf("with no list, titles %q, want Heat kept", got)
	}
}
