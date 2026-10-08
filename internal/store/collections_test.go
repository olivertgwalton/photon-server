//go:build integration

package store

import (
	"errors"
	"reflect"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

func TestBoxSetsAreMadeFromWhatAProviderSays(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]uuid.UUID{}
	for _, f := range []struct {
		title    string
		released time.Time
	}{{"Aliens", time.Date(1986, 7, 18, 0, 0, 0, 0, time.UTC)}, {"Alien", time.Date(1979, 5, 25, 0, 0, 0, 0, time.UTC)}, {"Heat", time.Date(1995, 12, 15, 0, 0, 0, 0, time.UTC)}} {
		film := Film{Title: f.title, Folder: f.title, Copies: []Copy{{ContentKey: []byte(f.title), Parts: []Part{{
			RelPath: f.title + ".mkv", Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{},
		}}}}}
		if _, err := s.SaveFolder(ctx, lib.ID, f.title, []byte("v1"), []Film{film}, nil); err != nil {
			t.Fatal(err)
		}
		cards, _, err := s.Wall(ctx, []uuid.UUID{lib.ID}, WallPage{Sort: domain.SortAdded, Order: domain.Descending, Limit: 1})
		if err != nil {
			t.Fatal(err)
		}
		ids[f.title] = cards[0].ID
		m := domain.Metadata{Title: f.title, ReleaseDate: f.released}
		if f.title != "Heat" {
			m.Collections = []domain.Grouping{{ID: "8091", Title: "Alien Collection", Artwork: []domain.Artwork{{Kind: domain.ArtworkPoster, URL: "https://image.tmdb.org/t/p/original/set.jpg"}}}}
		}
		if err := s.SaveIdentity(ctx, ids[f.title], domain.SourceTMDB, m, nil); err != nil {
			t.Fatal(err)
		}
	}
	shown, total, err := s.Collections(ctx, lib.ID, uuid.UUID{}, 0, 10)
	if err != nil || total != 1 || len(shown) != 1 || shown[0].Title != "Alien Collection" || shown[0].Poster == (uuid.UUID{}) {
		t.Fatalf("collections = %+v of %d, %v; want the one set with its poster", shown, total, err)
	}
	set := shown[0].ID
	members, err := s.Members(ctx, uuid.UUID{}, set)
	if err != nil || len(members) != 2 || members[0].Title != "Alien" || members[1].Title != "Aliens" {
		t.Errorf("members = %+v, %v; want Alien then Aliens, by release", members, err)
	}
	page, err := s.Title(ctx, uuid.UUID{}, ids["Alien"])
	if err != nil || !reflect.DeepEqual(page.Collections, []CollectionCard{{ID: set, Title: "Alien Collection", Poster: shown[0].Poster}}) {
		t.Errorf("Alien is in %v, %v; want the set with its poster", page.Collections, err)
	}
	if shown[0].Origin != domain.CollectionTMDB {
		t.Errorf("the set's card says it was made by %q, want tmdb", shown[0].Origin)
	}
	if page, err := s.Title(ctx, uuid.UUID{}, set); err != nil || page.Origin != domain.CollectionTMDB {
		t.Errorf("the set's page says it was made by %q, %v; want tmdb", page.Origin, err)
	}
	if page, _ := s.Title(ctx, uuid.UUID{}, set); page.Placement != domain.PlacementLibrary {
		t.Errorf("a new set is placed %q, want library", page.Placement)
	}
	// A provider's set is put on the home page as an admin's is.
	if err := s.SetPlacement(ctx, set, domain.PlacementHome); err != nil {
		t.Fatal(err)
	}
	if page, _ := s.Title(ctx, uuid.UUID{}, set); page.Placement != domain.PlacementHome {
		t.Errorf("the promoted set is placed %q, want home", page.Placement)
	}
	if err := s.SetPlacement(ctx, uuid.NewV7(), domain.PlacementHome); !errors.Is(err, ErrNotFound) {
		t.Errorf("placing no collection: %v, want ErrNotFound", err)
	}
	if err := s.SetMembers(ctx, set, nil); !errors.Is(err, ErrNotUserCollection) {
		t.Errorf("changing TMDB's set by hand: %v, want ErrNotUserCollection", err)
	}
	if err := s.RemoveCollection(ctx, set); !errors.Is(err, ErrNotUserCollection) {
		t.Errorf("removing TMDB's set by hand: %v, want ErrNotUserCollection", err)
	}
	// Its name and words are an admin's to edit, as any title's, and outlast TMDB saying them again.
	if err := s.EditMetadata(ctx, set, domain.Metadata{Title: "Alien Anthology"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveIdentity(ctx, ids["Alien"], domain.SourceTMDB, domain.Metadata{Title: "Alien", Collections: []domain.Grouping{{ID: "8091", Title: "Alien Collection"}}}, nil); err != nil {
		t.Fatal(err)
	}
	if page, err := s.Title(ctx, uuid.UUID{}, set); err != nil || page.Title != "Alien Anthology" {
		t.Errorf("the renamed set is called %q, %v; want the admin's name", page.Title, err)
	}

	// Aliens is no longer said to be in it: one is no set, and none is gone at the next scan.
	if err := s.SaveIdentity(ctx, ids["Aliens"], domain.SourceTMDB, domain.Metadata{Title: "Aliens"}, nil); err != nil {
		t.Fatal(err)
	}
	if shown, _, _ := s.Collections(ctx, lib.ID, uuid.UUID{}, 0, 10); len(shown) != 0 {
		t.Errorf("with one title left: %+v, want none shown", shown)
	}
	if err := s.SaveIdentity(ctx, ids["Alien"], domain.SourceTMDB, domain.Metadata{Title: "Alien"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.FinishScan(ctx, lib.ID, []string{"."}, []string{"Alien", "Aliens", "Heat"}, []string{"Alien.mkv", "Aliens.mkv", "Heat.mkv"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Members(ctx, uuid.UUID{}, set); !errors.Is(err, ErrNotFound) {
		t.Errorf("an emptied set after a scan: %v, want it gone", err)
	}

	// An admin's own, in their order, of any size.
	mine, err := s.AddCollection(ctx, lib.ID, "Favourites of 1979")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetMembers(ctx, mine, []uuid.UUID{ids["Heat"], ids["Alien"]}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetMembers(ctx, mine, []uuid.UUID{ids["Heat"], ids["Heat"]}); !errors.Is(err, ErrNotFound) {
		t.Errorf("a title twice: %v, want ErrNotFound", err)
	}
	if err := s.SetMembers(ctx, mine, []uuid.UUID{uuid.NewV7()}); !errors.Is(err, ErrNotFound) {
		t.Errorf("a title of no library: %v, want ErrNotFound", err)
	}
	if shown, _, err := s.Collections(ctx, lib.ID, uuid.UUID{}, 0, 10); err != nil || len(shown) != 1 || shown[0].Origin != domain.CollectionUser {
		t.Errorf("collections = %+v, %v; want the admin's own, saying so", shown, err)
	}
	members, err = s.Members(ctx, uuid.UUID{}, mine)
	if err != nil || len(members) != 2 || members[0].Title != "Heat" {
		t.Errorf("an admin's set = %+v, %v; want Heat first, as put", members, err)
	}
	viewer, err := s.AddProfile(ctx, "Viewer", domain.RoleUser, "hash")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.MarkWatched(ctx, viewer.ID, mine, nil); err != nil {
		t.Fatal(err)
	}
	if watched, _, _ := s.Wall(ctx, []uuid.UUID{lib.ID}, WallPage{Profile: viewer.ID, Sort: domain.SortTitle, Limit: 10, Filter: WallFilter{Marks: []domain.Mark{domain.MarkWatched}}}); len(watched) != 2 {
		t.Errorf("after marking the set watched, %d titles are, want both", len(watched))
	}
	if err := s.RemoveCollection(ctx, mine); err != nil {
		t.Fatal(err)
	}
	if page, err := s.Title(ctx, uuid.UUID{}, ids["Heat"]); err != nil || len(page.Collections) != 0 {
		t.Errorf("after removing it, Heat is in %v, %v", page.Collections, err)
	}
}

func TestALibrarysWallShowsItsCollectionsAsItSays(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]uuid.UUID{}
	for _, title := range []string{"Alien", "Aliens", "Heat"} {
		film := Film{Title: title, Folder: title, Copies: []Copy{{ContentKey: []byte(title), Parts: []Part{{
			RelPath: title + ".mkv", Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{},
		}}}}}
		if _, err := s.SaveFolder(ctx, lib.ID, title, []byte("v1"), []Film{film}, nil); err != nil {
			t.Fatal(err)
		}
		cards, _, err := s.Wall(ctx, []uuid.UUID{lib.ID}, WallPage{Sort: domain.SortAdded, Order: domain.Descending, Limit: 1})
		if err != nil {
			t.Fatal(err)
		}
		ids[title] = cards[0].ID
	}
	set, err := s.AddCollection(ctx, lib.ID, "Alien Anthology")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetMembers(ctx, set, []uuid.UUID{ids["Alien"], ids["Aliens"]}); err != nil {
		t.Fatal(err)
	}
	wall := func(f WallFilter) []string {
		cards, total, err := s.Wall(ctx, []uuid.UUID{lib.ID}, WallPage{Sort: domain.SortTitle, Limit: 10, Filter: f})
		if err != nil {
			t.Fatal(err)
		}
		letters, err := s.Letters(ctx, lib.ID, uuid.UUID{}, f)
		if err != nil {
			t.Fatal(err)
		}
		counted := 0
		for _, l := range letters {
			counted += l.Count
		}
		if int(total) != len(cards) || counted != len(cards) {
			t.Errorf("%d cards, %d in all, %d by letter; want them to agree", len(cards), total, counted)
		}
		var titles []string
		for _, c := range cards {
			titles = append(titles, c.Title)
		}
		return titles
	}
	for _, tc := range []struct {
		mode domain.CollectionMode
		want []string
	}{
		{domain.CollectionsGrouped, []string{"Alien Anthology", "Heat"}},
		{domain.CollectionsShown, []string{"Alien", "Alien Anthology", "Aliens", "Heat"}},
		{domain.CollectionsHidden, []string{"Alien", "Aliens", "Heat"}},
	} {
		if err := s.SetLibrary(ctx, lib.ID, LibraryChange{Collections: tc.mode}); err != nil {
			t.Fatal(err)
		}
		if got := wall(WallFilter{}); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: the wall shows %q, want %q", tc.mode, got, tc.want)
		}
	}
	// A filtered wall answers titles alone, those in a set among them.
	if err := s.SetLibrary(ctx, lib.ID, LibraryChange{Collections: domain.CollectionsGrouped}); err != nil {
		t.Fatal(err)
	}
	if got := wall(WallFilter{StartsWith: "A"}); !reflect.DeepEqual(got, []string{"Alien", "Aliens"}) {
		t.Errorf("filtered to A: the wall shows %q, want the films alone", got)
	}
}

func TestALibraryCountsTheCollectionsItLists(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	films, err := s.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	empty, err := s.AddLibrary(ctx, "Other", domain.LibraryMovies, "/srv/other")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddCollection(ctx, films.ID, "Sunday Films"); err != nil {
		t.Fatal(err)
	}
	// A provider's set of one title is not listed, so it is not counted either.
	film := Film{Title: "Alien", Folder: "Alien", Copies: []Copy{{ContentKey: []byte("Alien"), Parts: []Part{{
		RelPath: "Alien.mkv", Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{},
	}}}}}
	if _, err := s.SaveFolder(ctx, films.ID, "Alien", []byte("v1"), []Film{film}, nil); err != nil {
		t.Fatal(err)
	}
	cards, _, err := s.Wall(ctx, []uuid.UUID{films.ID}, WallPage{Sort: domain.SortAdded, Order: domain.Descending, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	m := domain.Metadata{Title: "Alien", Certificate: "18", Collections: []domain.Grouping{{ID: "8091", Title: "Alien Collection"}}}
	if err := s.SaveIdentity(ctx, cards[0].ID, domain.SourceTMDB, m, nil); err != nil {
		t.Fatal(err)
	}
	kid, err := s.AddProfile(ctx, "Kid", domain.RoleUser, "hash")
	if err != nil {
		t.Fatal(err)
	}
	agree := func(profile uuid.UUID, want map[uuid.UUID]int) {
		t.Helper()
		counts, err := s.LibraryCounts(ctx, profile)
		if err != nil {
			t.Fatal(err)
		}
		for lib, n := range want {
			_, listed, err := s.Collections(ctx, lib, profile, 0, 10)
			if err != nil || counts[lib].Collections != n || listed != int64(n) {
				t.Errorf("library %s counts %d collections and lists %d, %v; want %d", lib, counts[lib].Collections, listed, err, n)
			}
		}
	}
	agree(uuid.UUID{}, map[uuid.UUID]int{films.ID: 1, empty.ID: 0})
	twelve := 12
	if err := s.SetAccess(ctx, kid.ID, ProfileAccess{MaxAge: &twelve, Unrated: domain.UnratedBlock}); err != nil {
		t.Fatal(err)
	}
	agree(kid.ID, map[uuid.UUID]int{films.ID: 1, empty.ID: 0})
	if err := s.SetAccess(ctx, kid.ID, ProfileAccess{Libraries: []uuid.UUID{empty.ID}}); err != nil {
		t.Fatal(err)
	}
	agree(kid.ID, map[uuid.UUID]int{films.ID: 0, empty.ID: 0})
}

func TestACollectionOnTheHomePageIsARowOfItsTitles(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	films, err := s.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.AddLibrary(ctx, "Other", domain.LibraryMovies, "/srv/other")
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]uuid.UUID{}
	for _, title := range []string{"Alien", "Heat"} {
		film := Film{Title: title, Folder: title, Copies: []Copy{{ContentKey: []byte(title), Parts: []Part{{
			RelPath: title + ".mkv", Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{},
		}}}}}
		if _, err := s.SaveFolder(ctx, films.ID, title, []byte("v1"), []Film{film}, nil); err != nil {
			t.Fatal(err)
		}
		ids[title] = oneItem(t, s, `kind = 'movie' AND title = $1`, title).ID
	}
	set, err := s.AddCollection(ctx, films.ID, "Sunday Films")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetMembers(ctx, set, []uuid.UUID{ids["Heat"], ids["Alien"]}); err != nil {
		t.Fatal(err)
	}
	admin, err := s.AddProfile(ctx, "Admin", domain.RoleAdmin, "hash")
	if err != nil {
		t.Fatal(err)
	}
	kid, err := s.AddProfile(ctx, "Kid", domain.RoleUser, "hash")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetAccess(ctx, kid.ID, ProfileAccess{Libraries: []uuid.UUID{other.ID}}); err != nil {
		t.Fatal(err)
	}
	row := func(profile uuid.UUID) []string {
		t.Helper()
		rows, err := s.Home(ctx, profile, 10)
		if err != nil {
			t.Fatal(err)
		}
		var titles []string
		for _, r := range rows {
			if r.Kind != domain.RowCollection {
				continue
			}
			if r.Collection == nil || *r.Collection != (TitleRef{ID: set, Title: "Sunday Films"}) {
				t.Errorf("a collection row is %+v, want Sunday Films", r.Collection)
			}
			for _, c := range r.Cards {
				titles = append(titles, c.Title)
			}
		}
		return titles
	}
	if got := row(admin.ID); len(got) != 0 {
		t.Errorf("before it is promoted, the home has %v, want no collection row", got)
	}
	if err := s.SetPlacement(ctx, set, domain.PlacementHome); err != nil {
		t.Fatal(err)
	}
	if got := row(admin.ID); !reflect.DeepEqual(got, []string{"Heat", "Alien"}) {
		t.Errorf("the promoted row = %v, want Heat then Alien, as put", got)
	}
	if got := row(kid.ID); len(got) != 0 {
		t.Errorf("a profile without the library sees %v, want no row", got)
	}
	if err := s.SetPlacement(ctx, set, domain.PlacementLibrary); err != nil {
		t.Fatal(err)
	}
	if got := row(admin.ID); len(got) != 0 {
		t.Errorf("once it is back in its library, the home has %v, want no row", got)
	}
}

// A smart collection is its library's titles its rule finds, in its order, the same for everyone:
// found again as the library changes, shown with none, kept by a scan, and never changed by hand.
func TestASmartCollectionIsWhatItsRuleFinds(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	add := func(title string, released time.Time, genre string) {
		t.Helper()
		film := Film{Title: title, Folder: title, Copies: []Copy{{ContentKey: []byte(title), Parts: []Part{{
			RelPath: title + ".mkv", Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{},
		}}}}}
		if _, err := s.SaveFolder(ctx, lib.ID, title, []byte("v1"), []Film{film}, nil); err != nil {
			t.Fatal(err)
		}
		cards, _, err := s.Wall(ctx, []uuid.UUID{lib.ID}, WallPage{Sort: domain.SortAdded, Order: domain.Descending, Limit: 1})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.SaveIdentity(ctx, cards[0].ID, domain.SourceTMDB, domain.Metadata{Title: title, ReleaseDate: released, Genres: []string{genre}}, nil); err != nil {
			t.Fatal(err)
		}
	}
	year := func(y int) time.Time { return time.Date(y, 6, 1, 0, 0, 0, 0, time.UTC) }
	add("Airplane!", year(1980), "Comedy")
	add("Heat", year(1995), "Crime")
	add("Groundhog Day", year(1993), "Comedy")

	rule := SmartRule{Filter: WallFilter{Genres: []string{"Comedy"}}, Sort: domain.SortReleased, Order: domain.Descending}
	set, err := s.AddSmartCollection(ctx, lib.ID, "Comedies", rule)
	if err != nil {
		t.Fatal(err)
	}
	titles := func() []string {
		t.Helper()
		members, err := s.Members(ctx, uuid.UUID{}, set)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, m := range members {
			out = append(out, m.Title)
		}
		return out
	}
	if got := titles(); !reflect.DeepEqual(got, []string{"Groundhog Day", "Airplane!"}) {
		t.Errorf("members %v, want the comedies, newest first", got)
	}

	add("Barbie", year(2023), "Comedy")
	if err := s.RefreshSmartCollections(ctx, lib.ID); err != nil {
		t.Fatal(err)
	}
	if got := titles(); !reflect.DeepEqual(got, []string{"Barbie", "Groundhog Day", "Airplane!"}) {
		t.Errorf("found again: %v, want the comedy added first", got)
	}

	rule.Limit, rule.Order = 1, domain.Ascending
	if err := s.SetRule(ctx, set, rule); err != nil {
		t.Fatal(err)
	}
	if got := titles(); !reflect.DeepEqual(got, []string{"Airplane!"}) {
		t.Errorf("the first comedy alone: %v", got)
	}
	if page, err := s.Title(ctx, uuid.UUID{}, set); err != nil || page.Origin != domain.CollectionSmart || page.Rule == nil || page.Rule.Limit != 1 {
		t.Errorf("its page: %+v, %v; want it smart, with its rule", page.Rule, err)
	}

	// Finding nothing, it is shown still, and a scan keeps it.
	if err := s.SetRule(ctx, set, SmartRule{Filter: WallFilter{Genres: []string{"Western"}}}); err != nil {
		t.Fatal(err)
	}
	add("Heat", year(1995), "Crime")
	if shown, total, err := s.Collections(ctx, lib.ID, uuid.UUID{}, 0, 10); err != nil || total != 1 || shown[0].ID != set {
		t.Errorf("collections %+v of %d, %v; want the empty smart one shown", shown, total, err)
	}
	if err := s.SetMembers(ctx, set, nil); !errors.Is(err, ErrNotUserCollection) {
		t.Errorf("its titles set by hand: %v, want ErrNotUserCollection", err)
	}
	if err := s.SetRule(ctx, set, SmartRule{Filter: WallFilter{Marks: []domain.Mark{domain.MarkUnwatched}}}); !errors.Is(err, ErrRuleForSomeone) {
		t.Errorf("a rule of marks: %v, want ErrRuleForSomeone", err)
	}
	if err := s.RemoveCollection(ctx, set); err != nil {
		t.Errorf("removing it: %v", err)
	}
}

// A list collection is the titles of its library a list holds, found by their TMDB or IMDb id,
// in the list's order, each once; it counts what the library lacks, and is read again as asked.
func TestAListCollectionHoldsWhatTheLibraryHasOfItsList(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	for title, ids := range map[string]map[domain.Provider]string{
		"Alien":  {domain.ProviderTMDB: "348", domain.ProviderIMDb: "tt0078748"},
		"Aliens": {domain.ProviderTMDB: "679", domain.ProviderIMDb: "tt0090605"},
		"Heat":   {domain.ProviderTMDB: "949"},
	} {
		film := Film{Title: title, Folder: title, Copies: []Copy{{ContentKey: []byte(title), Parts: []Part{{
			RelPath: title + ".mkv", Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{},
		}}}}}
		if _, err := s.SaveFolder(ctx, lib.ID, title, []byte("v1"), []Film{film}, nil); err != nil {
			t.Fatal(err)
		}
		cards, _, err := s.Wall(ctx, []uuid.UUID{lib.ID}, WallPage{Sort: domain.SortAdded, Order: domain.Descending, Limit: 1})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.SaveIdentity(ctx, cards[0].ID, domain.SourceTMDB, domain.Metadata{Title: title, IDs: ids}, nil); err != nil {
			t.Fatal(err)
		}
	}
	film := func(ids ...string) domain.Listed {
		l := domain.Listed{Kind: domain.ItemMovie, IDs: map[domain.Provider]string{}}
		for i := 0; i < len(ids); i += 2 {
			l.IDs[domain.Provider(ids[i])] = ids[i+1]
		}
		return l
	}
	listed := []domain.Listed{
		film("tmdb", "679"),       // Aliens
		film("imdb", "tt0078748"), // Alien, by IMDb alone
		film("tmdb", "1091"),      // The Thing, not in the library
		{Kind: domain.ItemShow, IDs: map[domain.Provider]string{domain.ProviderTMDB: "949"}}, // a show of Heat's id
		film("tmdb", "679", "imdb", "tt0090605"),                                             // Aliens again
	}
	list := ListRef{Source: domain.SourceTMDB, ID: "8136"}
	set, err := s.AddListCollection(ctx, lib.ID, "Alien films", list, listed)
	if err != nil {
		t.Fatal(err)
	}
	members, err := s.Members(ctx, uuid.UUID{}, set)
	if err != nil || len(members) != 2 || members[0].Title != "Aliens" || members[1].Title != "Alien" {
		t.Errorf("members %+v, %v; want Aliens then Alien, as listed", members, err)
	}
	if got, err := s.CollectionList(ctx, set); err != nil || got.Source != domain.SourceTMDB || got.ID != "8136" || got.Missing != 2 {
		t.Errorf("its list %+v, %v; want TMDB's 8136, two of it missing", got, err)
	}

	if err := s.SetListMembers(ctx, set, []domain.Listed{film("tmdb", "348")}); err != nil {
		t.Fatal(err)
	}
	if members, _ := s.Members(ctx, uuid.UUID{}, set); len(members) != 1 || members[0].Title != "Alien" {
		t.Errorf("read again: %+v, want Alien alone", members)
	}
	if lists, err := s.ListCollections(ctx); err != nil || len(lists) != 1 || lists[0].ID != set || lists[0].List.Missing != 0 {
		t.Errorf("list collections %+v, %v", lists, err)
	}
	if err := s.SetMembers(ctx, set, nil); !errors.Is(err, ErrNotUserCollection) {
		t.Errorf("its titles set by hand: %v, want ErrNotUserCollection", err)
	}
	if err := s.RemoveCollection(ctx, set); err != nil {
		t.Errorf("removing it: %v", err)
	}
}
