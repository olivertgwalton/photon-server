//go:build integration

package store

import (
	"cmp"
	"errors"
	"slices"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
)

func TestWallPagesEveryTitleOnce(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// Two titles share a sort title and two share a time added, so the id breaks both ties.
	// Released: a date, a year alone (its first of January), or neither, which goes last.
	date := func(y, m, d int) *time.Time { return new(time.Date(y, time.Month(m), d, 0, 0, 0, 0, time.UTC)) }
	for n, f := range []struct {
		title    string
		year     int
		released *time.Time
	}{
		{"Heat", 1995, date(1995, 12, 15)},
		{"Alien", 1979, nil},
		{"heat", 0, nil},
		{"Brazil", 1985, date(1985, 2, 20)},
		{"Zodiac", 0, date(2007, 3, 2)},
		{"Memento", 2000, date(2000, 9, 5)},
		{"Ran", 1985, nil},
	} {
		item := model.Item{
			LibraryID: &lib.ID, Kind: domain.ItemMovie, Title: f.title, ScanTitle: f.title,
			SortTitle: sortTitle(f.title), Folder: f.title, AddedAt: base.Add(time.Duration(n%5) * time.Hour),
			ReleaseDate: f.released,
		}
		if f.year != 0 {
			item.Year = &f.year
		}
		id := addItem(t, s, item)
		if f.title == "Heat" {
			addItem(t, s, model.Item{
				LibraryID: &lib.ID, Kind: domain.ItemExtra, ParentID: &id, ExtraKind: new(domain.ExtraTrailer), Title: "Trailer",
				ScanTitle: "Trailer", SortTitle: "trailer", Folder: "Heat",
			})
		}
	}

	for _, sort := range domain.WallSorts() {
		for _, order := range []domain.Order{domain.Ascending, domain.Descending} {
			full, total, err := s.Wall(ctx, []uuid.UUID{lib.ID}, WallPage{Sort: sort, Order: order, Limit: 100})
			if err != nil || total != 7 || len(full) != 7 {
				t.Fatalf("%s %s in one page: %d titles of %d, err %v; want the 7 films", sort, order, len(full), total, err)
			}
			var paged []Card
			for offset := 0; offset < int(total); offset += 3 {
				page, n, err := s.Wall(ctx, []uuid.UUID{lib.ID}, WallPage{Sort: sort, Order: order, Offset: offset, Limit: 3})
				if err != nil || n != total {
					t.Fatalf("from %d: %d of %d, %v", offset, len(page), n, err)
				}
				paged = append(paged, page...)
			}
			ids := func(cs []Card) []uuid.UUID {
				out := make([]uuid.UUID, len(cs))
				for n, c := range cs {
					out[n] = c.ID
				}
				return out
			}
			if !slices.Equal(ids(paged), ids(full)) {
				t.Errorf("%s %s: paging by 3 gave %v, want %v", sort, order, ids(paged), ids(full))
			}
			if sort == domain.SortReleased {
				var titles []string
				for _, c := range full {
					titles = append(titles, c.Title)
				}
				want := []string{"Alien", "Ran", "Brazil", "Heat", "Memento", "Zodiac", "heat"}
				if order == domain.Descending {
					want = []string{"Zodiac", "Memento", "Heat", "Brazil", "Ran", "Alien", "heat"}
				}
				if !slices.Equal(titles, want) {
					t.Errorf("released %s = %q, want %q", order, titles, want)
				}
			}
		}
	}

	if past, total, err := s.Wall(ctx, []uuid.UUID{lib.ID}, WallPage{Sort: domain.SortTitle, Offset: 7, Limit: 3}); err != nil || len(past) != 0 || total != 7 {
		t.Errorf("past the end: %d of %d, %v; want none of 7", len(past), total, err)
	}
	// Alien, Brazil, Heat and heat, Memento, Ran, Zodiac.
	letters, err := s.Letters(ctx, lib.ID, uuid.UUID{}, domain.WallTitles, WallFilter{})
	want := []Letter{{"A", 1}, {"B", 1}, {"H", 2}, {"M", 1}, {"R", 1}, {"Z", 1}}
	if err != nil || !slices.Equal(letters, want) {
		t.Errorf("letters = %v, %v; want %v", letters, err, want)
	}
	if _, err := s.Letters(ctx, uuid.NewV7(), uuid.UUID{}, domain.WallTitles, WallFilter{}); !errors.Is(err, ErrNotFound) {
		t.Errorf("letters of an unknown library: %v, want ErrNotFound", err)
	}
	if _, _, err := s.Wall(ctx, []uuid.UUID{uuid.NewV7()}, WallPage{Sort: domain.SortTitle, Limit: 3}); !errors.Is(err, ErrNotFound) {
		t.Errorf("an unknown library: err = %v, want ErrNotFound", err)
	}
}

// Numbers in titles are read as numbers, as Jellyfin sorts them.
func TestTitlesSortTheirNumbersAsNumbers(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	for _, title := range []string{"1917", "The Age of Adaline", "13 Going on 30", "2 Fast 2 Furious", "Alien", "21 Jump Street"} {
		addItem(t, s, model.Item{
			LibraryID: &lib.ID, Kind: domain.ItemMovie, Title: title, ScanTitle: title,
			SortTitle: sortTitle(title), Folder: title,
		})
	}
	page, _, err := s.Wall(ctx, []uuid.UUID{lib.ID}, WallPage{Sort: domain.SortTitle, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	var titles []string
	for _, c := range page {
		titles = append(titles, c.Title)
	}
	want := []string{"2 Fast 2 Furious", "13 Going on 30", "21 Jump Street", "1917", "The Age of Adaline", "Alien"}
	if !slices.Equal(titles, want) {
		t.Errorf("by title = %q, want %q", titles, want)
	}
}

// Libraries are read as one wall, their titles sorted and paged together, as an app asks for
// every library's films and shows at once.
func TestOneWallOfSeveralLibraries(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	admin, _ := homeLibraries(t, s, []string{"Alien", "Heat"}, []string{"Fargo"})
	libs, err := s.LibrariesSeen(ctx, admin)
	if err != nil || len(libs) != 2 {
		t.Fatalf("libraries %v, %v", libs, err)
	}
	ids := []uuid.UUID{libs[0].ID, libs[1].ID}
	titles := func(offset, limit int) ([]string, int64) {
		t.Helper()
		cards, total, err := s.Wall(ctx, ids, WallPage{Profile: admin, Sort: domain.SortTitle, Offset: offset, Limit: limit})
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, c := range cards {
			out = append(out, c.Title)
		}
		return out, total
	}
	if got, total := titles(0, 10); !slices.Equal(got, []string{"Alien", "Fargo", "Heat"}) || total != 3 {
		t.Errorf("both libraries: %v of %d", got, total)
	}
	if got, _ := titles(1, 1); !slices.Equal(got, []string{"Fargo"}) {
		t.Errorf("the second title of both: %v", got)
	}
}

// Titles asked for by id come in the order asked, each once, and only those the profile may see.
func TestCardsAreTheTitlesAskedForByID(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	admin, kid := homeLibraries(t, s, []string{"Alien", "Heat"}, []string{"Fargo"})
	heat, fargo := oneItem(t, s, "title = 'Heat'").ID, oneItem(t, s, "kind = 'show'").ID
	episode := oneItem(t, s, "kind = 'episode'").ID
	titles := func(profile uuid.UUID, ids ...uuid.UUID) []string {
		t.Helper()
		cards, err := s.Cards(ctx, profile, ids)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, c := range cards {
			out = append(out, c.Title)
		}
		return out
	}
	if got := titles(admin, fargo, uuid.NewV7(), heat, fargo, episode); !slices.Equal(got, []string{"Fargo", "Heat", "Fargo S1E1"}) {
		t.Errorf("by id: %v", got)
	}
	if got := titles(kid, fargo, heat); !slices.Equal(got, []string{"Heat"}) {
		t.Errorf("for a profile without the shows: %v, want the film alone", got)
	}
}

// A show library's episodes are paged across its shows: those that came out the same day in their
// shows' order, the undated last, each show read as its show is.
func TestAWallPagesEpisodesAcrossShows(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "Shows", domain.LibraryShows, "/srv/shows")
	if err != nil {
		t.Fatal(err)
	}
	films, err := s.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	day := func(d int) *time.Time { return new(time.Date(2026, 1, d, 0, 0, 0, 0, time.UTC)) }
	type episode struct {
		title           string
		season, episode int
		aired           *time.Time
	}
	ids := map[string]uuid.UUID{}
	for _, show := range []struct {
		title, cert, genre string
		episodes           []episode
	}{
		// Added first, so the show's id can't put it first.
		{"The Wire", "18", "Drama", []episode{{"w2", 1, 2, day(12)}, {"w1", 1, 1, day(5)}}},
		{"Archer", "15", "Comedy", []episode{{"a3", 2, 1, nil}, {"a2", 1, 2, day(5)}, {"a1", 1, 1, day(5)}}},
	} {
		showID := addItem(t, s, model.Item{
			LibraryID: &lib.ID, Kind: domain.ItemShow, Title: show.title, ScanTitle: show.title, SortTitle: sortTitle(show.title),
			Folder: show.title, Certificate: &show.cert, Genres: []string{show.genre},
		})
		seasons := map[int]uuid.UUID{}
		for _, e := range show.episodes {
			if _, ok := seasons[e.season]; !ok {
				seasons[e.season] = addItem(t, s, model.Item{
					LibraryID: &lib.ID, Kind: domain.ItemSeason, ParentID: &showID, SeasonNumber: &e.season,
					Title: "Season", ScanTitle: "Season", SortTitle: "season", Folder: show.title,
				})
			}
			ids[e.title] = addItem(t, s, model.Item{
				LibraryID: &lib.ID, Kind: domain.ItemEpisode, ParentID: new(seasons[e.season]), SeasonNumber: &e.season,
				EpisodeNumber: &e.episode, AirDate: e.aired, Title: e.title, ScanTitle: e.title, SortTitle: e.title, Folder: show.title,
			})
		}
	}
	oliver, err := s.AddProfile(ctx, "Oliver", domain.RoleAdmin, "hash", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.MarkWatched(ctx, oliver.ID, ids["a1"], nil); err != nil {
		t.Fatal(err)
	}
	titles := func(cards []Card) []string {
		out := []string{}
		for _, c := range cards {
			out = append(out, c.Title)
		}
		return out
	}
	episodes := func(p WallPage) []string {
		t.Helper()
		p.Profile, p.Kind, p.Limit = cmp.Or(p.Profile, oliver.ID), domain.WallEpisodes, 100
		cards, total, err := s.Wall(ctx, []uuid.UUID{lib.ID}, p)
		if err != nil || int(total) != len(cards) {
			t.Fatalf("%+v: %d of %d, %v", p, len(cards), total, err)
		}
		return titles(cards)
	}

	for _, tc := range []struct {
		name string
		page WallPage
		want []string
	}{
		{"by show", WallPage{Sort: domain.SortTitle}, []string{"a1", "a2", "a3", "w1", "w2"}},
		{"by show, Z to A", WallPage{Sort: domain.SortTitle, Order: domain.Descending}, []string{"w2", "w1", "a3", "a2", "a1"}},
		{"oldest first", WallPage{Sort: domain.SortReleased}, []string{"a1", "a2", "w1", "w2", "a3"}},
		{"newest first", WallPage{Sort: domain.SortReleased, Order: domain.Descending}, []string{"w2", "w1", "a2", "a1", "a3"}},
		{"a drama, by its show", WallPage{Sort: domain.SortTitle, Filter: WallFilter{Genres: []string{"Drama"}}}, []string{"w1", "w2"}},
		{"unwatched", WallPage{Sort: domain.SortTitle, Filter: WallFilter{Marks: []domain.Mark{domain.MarkUnwatched}}}, []string{"a2", "a3", "w1", "w2"}},
		{"under W", WallPage{Sort: domain.SortTitle, Filter: WallFilter{StartsWith: "W"}}, []string{"w1", "w2"}},
		{"last played first, the unplayed by show", WallPage{Sort: domain.SortPlayed, Order: domain.Descending}, []string{"a1", "w2", "w1", "a3", "a2"}},
	} {
		if got := episodes(tc.page); !slices.Equal(got, tc.want) {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
	// Every sort pages the same episodes in the same order a page at a time.
	for _, sort := range domain.WallSorts() {
		for _, order := range domain.Orders() {
			full := episodes(WallPage{Sort: sort, Order: order})
			var paged []string
			for offset := 0; offset < len(full); offset += 2 {
				page, _, err := s.Wall(ctx, []uuid.UUID{lib.ID}, WallPage{Profile: oliver.ID, Kind: domain.WallEpisodes, Sort: sort, Order: order, Offset: offset, Limit: 2})
				if err != nil {
					t.Fatal(err)
				}
				paged = append(paged, titles(page)...)
			}
			if len(full) != 5 || !slices.Equal(paged, full) {
				t.Errorf("%s %s: paging by 2 gave %q, want %q", sort, order, paged, full)
			}
		}
	}

	// A profile kept to 15 sees The Wire's episodes no more than The Wire.
	teen, err := s.AddProfile(ctx, "Teen", domain.RoleUser, "hash", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetAccess(ctx, teen.ID, ProfileAccess{MaxAge: new(15), Unrated: domain.UnratedAllow}, nil); err != nil {
		t.Fatal(err)
	}
	if got := episodes(WallPage{Profile: teen.ID, Sort: domain.SortTitle}); !slices.Equal(got, []string{"a1", "a2", "a3"}) {
		t.Errorf("kept to 15: %q, want Archer's", got)
	}
	letters, err := s.Letters(ctx, lib.ID, oliver.ID, domain.WallEpisodes, WallFilter{})
	if want := []Letter{{"A", 3}, {"W", 2}}; err != nil || !slices.Equal(letters, want) {
		t.Errorf("letters = %v, %v; want %v, by show", letters, err, want)
	}
	facets, err := s.Facets(ctx, []uuid.UUID{lib.ID}, oliver.ID, domain.WallEpisodes)
	if err != nil || !slices.Equal(facets.Genres, []string{"Comedy", "Drama"}) || !slices.Equal(facets.Certificates, []string{"15", "18"}) {
		t.Errorf("facets = %+v, %v; want their shows' genres and certificates", facets, err)
	}
	if cards, total, err := s.Wall(ctx, []uuid.UUID{films.ID}, WallPage{Kind: domain.WallEpisodes, Sort: domain.SortTitle, Limit: 10}); err != nil || total != 0 || len(cards) != 0 {
		t.Errorf("a film library's episodes: %d of %d, %v; want none", len(cards), total, err)
	}
}

// A mixed library's wall pages its films and shows together, or either alone, and its episodes;
// it has both a film library's and a show library's recently added rows.
func TestAMixedLibrarysWallHoldsFilmsAndShows(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddRemoteLibrary(ctx, "Popular", domain.LibraryMixed, Remote{ListSource: domain.PluginSource("aio"), ListID: "top", StreamSource: domain.PluginSource("aio")})
	if err != nil {
		t.Fatal(err)
	}
	listed := []domain.Listed{
		{Kind: domain.ItemMovie, IDs: map[domain.Provider]string{domain.ProviderIMDb: "tt0113277"}, Title: "Heat"},
		{Kind: domain.ItemShow, IDs: map[domain.Provider]string{domain.ProviderIMDb: "tt0306414"}, Title: "The Wire"},
	}
	if _, err := s.SaveListed(ctx, lib.ID, domain.LibraryMixed.Titles(), listed); err != nil {
		t.Fatal(err)
	}
	admin, err := s.AddProfile(ctx, "Oliver", domain.RoleAdmin, "hash", nil)
	if err != nil {
		t.Fatal(err)
	}
	titles := func(kind domain.WallKind) []string {
		t.Helper()
		cards, _, err := s.Wall(ctx, []uuid.UUID{lib.ID}, WallPage{Profile: admin.ID, Kind: kind, Sort: domain.SortTitle, Limit: 10})
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, c := range cards {
			out = append(out, c.Title)
		}
		return out
	}
	for kind, want := range map[domain.WallKind][]string{
		domain.WallTitles: {"Heat", "The Wire"}, domain.WallFilms: {"Heat"}, domain.WallShows: {"The Wire"}, domain.WallEpisodes: nil,
	} {
		if got := titles(kind); !slices.Equal(got, want) {
			t.Errorf("a wall of %s: %v, want %v", kind, got, want)
		}
	}
	seen := &SeenLibrary{ID: lib.ID, Name: "Popular", Kind: domain.LibraryMixed}
	if cards, err := s.LibraryRow(ctx, admin.ID, domain.RowRecentFilms, seen, 10); err != nil || len(cards) != 1 || cards[0].Title != "Heat" {
		t.Errorf("recently added films: %v, %v; want Heat", cards, err)
	}
	// A show is there by its newest episode, which a listed show has none of until it is matched.
	if _, err := s.LibraryRow(ctx, admin.ID, domain.RowRecentShows, seen, 10); err != nil {
		t.Errorf("recently added shows: %v", err)
	}
	// A list that drops a kind of title drops it alone.
	if _, err := s.SaveListed(ctx, lib.ID, domain.LibraryMixed.Titles(), listed[:1]); err != nil {
		t.Fatal(err)
	}
	if got := titles(domain.WallTitles); !slices.Equal(got, []string{"Heat"}) {
		t.Errorf("after The Wire left the list: %v", got)
	}
}
