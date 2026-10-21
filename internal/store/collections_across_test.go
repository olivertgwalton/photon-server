//go:build integration

package store

import (
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
)

// A collection is in no library, as a Jellyfin box set: it holds the titles of any, a provider's
// is made once whichever libraries its films are in, and each library lists and finds the
// collections holding its titles: a provider's once it holds two of them.
func TestACollectionHoldsTheTitlesOfEveryLibrary(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	films, err := s.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films", "")
	if err != nil {
		t.Fatal(err)
	}
	uhd, err := s.AddLibrary(ctx, "Films in 4K", domain.LibraryMovies, "/srv/4k", "")
	if err != nil {
		t.Fatal(err)
	}
	shows, err := s.AddLibrary(ctx, "Shows", domain.LibraryShows, "/srv/shows", "")
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]uuid.UUID{}
	for _, f := range []struct {
		title string
		lib   uuid.UUID
		set   bool
	}{{"Alien", films.ID, true}, {"Alien 3", films.ID, true}, {"Heat", films.ID, false}, {"Aliens", uhd.ID, true}} {
		film := Film{Title: f.title, Folder: f.title, Copies: []Copy{{ContentKey: []byte(f.title), Parts: []Part{{
			RelPath: f.title + ".mkv", Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{},
		}}}}}
		if _, err := s.SaveFolder(ctx, f.lib, f.title, []byte("v1"), []Film{film}, nil); err != nil {
			t.Fatal(err)
		}
		ids[f.title] = oneItem(t, s, `title = '`+f.title+`'`).ID
		m := domain.Metadata{Title: f.title}
		if f.set {
			m.Collections = []domain.Grouping{{ID: "8091", Title: "Alien Collection"}}
		}
		if err := s.SaveIdentity(ctx, ids[f.title], domain.SourceTMDB, m, nil); err != nil {
			t.Fatal(err)
		}
	}
	ids["The Wire"] = addItem(t, s, model.Item{
		LibraryID: &shows.ID, Kind: domain.ItemShow, Title: "The Wire", ScanTitle: "The Wire", SortTitle: sortTitle("The Wire"), Folder: "The Wire",
	})
	weekend, err := s.AddCollection(ctx, "Weekend")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetMembers(ctx, weekend, []uuid.UUID{ids["Heat"], ids["The Wire"]}); err != nil {
		t.Fatalf("a film and a show of two libraries: %v", err)
	}

	titles := func(cards []Card) []string {
		out := []string{}
		for _, c := range cards {
			out = append(out, c.Title)
		}
		return out
	}
	all, _, err := totalled(s.AllCollections(ctx, uuid.UUID{}, totalledPage(0, 10)))
	if got := titles(all); err != nil || !slices.Equal(got, []string{"Alien Collection", "Weekend"}) {
		t.Errorf("the Collections view = %q, %v; want TMDB's box set once, and the admin's", got, err)
	}
	counts, err := s.LibraryCounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for lib, want := range map[uuid.UUID][]string{
		films.ID: {"Alien Collection", "Weekend"}, uhd.ID: {}, shows.ID: {"Weekend"},
	} {
		listed, _, err := totalled(s.Collections(ctx, lib, uuid.UUID{}, totalledPage(0, 10)))
		if got := titles(listed); err != nil || !slices.Equal(got, want) {
			t.Errorf("library %v lists %q, %v; want %q", lib, got, err, want)
		}
		if n := counts[lib].Collections; n != len(want) {
			t.Errorf("library %v counts %d collections, lists %d", lib, n, len(want))
		}
	}
	// A smart collection reads the walls of the libraries it names, together.
	smart, err := s.AddSmartCollection(ctx, []uuid.UUID{films.ID, uhd.ID}, "A films", SmartRule{Filter: WallFilter{StartsWith: "A"}})
	if err != nil {
		t.Fatal(err)
	}
	members, err := s.Members(ctx, uuid.UUID{}, smart)
	if got := titles(members); err != nil || !slices.Equal(got, []string{"Alien", "Alien 3", "Aliens"}) {
		t.Errorf("a smart collection of two libraries holds %q, %v; want both libraries' A films", got, err)
	}
	found, _, err := totalled(s.Search(ctx, SearchQuery{Text: "weekend", Library: shows.ID, Limit: 10, Count: domain.CountExact}))
	if got := titles(found); err != nil || !slices.Equal(got, []string{"Weekend"}) {
		t.Errorf("searching the shows for it = %q, %v; want the collection holding one", got, err)
	}
	if found, _, err = totalled(s.Search(ctx, SearchQuery{Text: "weekend", Library: uhd.ID, Limit: 10, Count: domain.CountExact})); err != nil || len(found) != 0 {
		t.Errorf("searching the 4K films for it = %q, %v; want none", titles(found), err)
	}
}

// Films matched at once, as a refresh matches them, make their box set once.
func TestFilmsMatchedAtOnceMakeOneBoxSet(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	films, err := s.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films", "")
	if err != nil {
		t.Fatal(err)
	}
	var ids []uuid.UUID
	for i := range 8 {
		title := "Avengers " + strconv.Itoa(i)
		ids = append(ids, addItem(t, s, model.Item{
			LibraryID: &films.ID, Kind: domain.ItemMovie, Title: title, ScanTitle: title, SortTitle: sortTitle(title), Folder: title,
		}))
	}
	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Go(func() {
			m := domain.Metadata{Title: "Avengers", Collections: []domain.Grouping{{ID: "86311", Title: "The Avengers Collection"}}}
			if err := s.SaveIdentity(ctx, id, domain.SourceTMDB, m, nil); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	var sets, held int
	if err := s.pool.QueryRow(ctx, `
		SELECT count(DISTINCT c.item_id), count(m.item_id) FROM collections c
		LEFT JOIN collection_members m ON m.collection_id = c.item_id WHERE c.origin = 'tmdb'`).Scan(&sets, &held); err != nil {
		t.Fatal(err)
	}
	if sets != 1 || held != len(ids) {
		t.Errorf("%d box sets holding %d films, want 1 holding %d", sets, held, len(ids))
	}
}
