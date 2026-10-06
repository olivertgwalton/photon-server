//go:build integration

package store

import (
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

func TestHome(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	films, err := s.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	tv, err := s.AddLibrary(ctx, "TV", domain.LibraryShows, "/srv/tv")
	if err != nil {
		t.Fatal(err)
	}
	profile, err := s.AddProfile(ctx, "Oliver", domain.RoleAdmin, "hash")
	if err != nil {
		t.Fatal(err)
	}
	part := func(rel string) []Copy {
		return []Copy{{ContentKey: []byte(rel), Parts: []Part{{
			RelPath: rel, Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{Duration: time.Hour},
		}}}}
	}
	if _, err := s.SaveFolder(ctx, films.ID, "Heat", []byte("v"), []Film{{Title: "Heat", Folder: "Heat", Copies: part("Heat/Heat.mkv")}}, nil); err != nil {
		t.Fatal(err)
	}
	var eps []Episode
	for season, numbers := range map[int][]int{0: {1}, 1: {1, 2, 3}} {
		for _, n := range numbers {
			rel := "Wire/S" + string(rune('0'+season)) + "E" + string(rune('0'+n)) + ".mkv"
			eps = append(eps, Episode{Season: season, Episodes: []int{n}, Title: rel, Folder: "Wire", ByNumber: true, Copies: part(rel)})
		}
	}
	if _, err := s.SaveShowFolder(ctx, tv.ID, "Wire", []byte("v"), Show{Title: "The Wire", Folder: "Wire"}, eps, nil); err != nil {
		t.Fatal(err)
	}
	episode := func(season, n int) uuid.UUID {
		t.Helper()
		return oneItem(t, s, `kind = 'episode' AND season_number = $1 AND episode_number = $2`, season, n).ID
	}
	home := func() map[domain.HomeRow][]string {
		t.Helper()
		rows, err := s.Home(ctx, profile.ID, 10)
		if err != nil {
			t.Fatal(err)
		}
		out := map[domain.HomeRow][]string{}
		for _, r := range rows {
			for _, c := range r.Cards {
				out[r.Kind] = append(out[r.Kind], c.Title)
			}
		}
		return out
	}

	if got := home(); len(got[domain.RowNextUp]) != 0 || len(got[domain.RowContinueWatching]) != 0 ||
		len(got[domain.RowRecentFilms]) != 1 || len(got[domain.RowRecentShows]) != 1 {
		t.Errorf("a new profile's home = %v, want only what was added", got)
	}

	if err := s.MarkWatched(ctx, profile.ID, episode(1, 1), nil); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkWatched(ctx, profile.ID, episode(0, 1), nil); err != nil {
		t.Fatal(err)
	}
	if got := home()[domain.RowNextUp]; len(got) != 1 || got[0] != "Wire/S1E2.mkv" {
		t.Errorf("next up = %v, want the episode after the last one watched, specials aside", got)
	}

	if _, err := s.SaveProgress(ctx, profile.ID, episode(1, 2), 20*time.Minute, domain.ReachStart, nil); err != nil {
		t.Fatal(err)
	}
	heat := oneItem(t, s, `kind = 'movie'`).ID
	if _, err := s.SaveProgress(ctx, profile.ID, heat, 30*time.Minute, domain.ReachStart, nil); err != nil {
		t.Fatal(err)
	}
	got := home()
	if c := got[domain.RowContinueWatching]; len(c) != 2 || c[0] != "Heat" {
		t.Errorf("continue watching = %v, want Heat, played last, then the episode", c)
	}
	if len(got[domain.RowNextUp]) != 0 {
		t.Errorf("next up = %v, want nothing: the next episode is under way", got[domain.RowNextUp])
	}

	if err := s.ClearProgress(ctx, profile.ID, heat); err != nil {
		t.Fatal(err)
	}
	if err := s.ClearProgress(ctx, profile.ID, uuid.NewV7()); !errors.Is(err, ErrNotFound) {
		t.Errorf("clearing a title there is not: err = %v, want %v", err, ErrNotFound)
	}
	if c := home()[domain.RowContinueWatching]; len(c) != 1 || c[0] != "Wire/S1E2.mkv" {
		t.Errorf("continue watching = %v, want Heat gone and the episode left", c)
	}
	show := oneItem(t, s, `kind = 'show'`).ID
	if err := s.ClearProgress(ctx, profile.ID, show); err != nil {
		t.Fatal(err)
	}
	got = home()
	if c := got[domain.RowContinueWatching]; len(c) != 0 {
		t.Errorf("continue watching = %v, want nothing once the show's progress is cleared", c)
	}
	if c := got[domain.RowNextUp]; len(c) != 1 || c[0] != "Wire/S1E2.mkv" {
		t.Errorf("next up = %v, want the episode after the one still watched", c)
	}
	page, err := s.Title(ctx, profile.ID, heat)
	if err != nil {
		t.Fatal(err)
	}
	if page.State.LastPlayedAt == nil || page.State.PositionMS != 0 {
		t.Errorf("Heat's state = %+v, want played but nowhere to resume", page.State)
	}
	page, err = s.Title(ctx, profile.ID, episode(1, 1))
	if err != nil {
		t.Fatal(err)
	}
	if page.State.WatchedAt == nil || page.State.Plays != 1 {
		t.Errorf("a watched episode's state = %+v, want it still watched once", page.State)
	}
	rows, err := s.Home(ctx, profile.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		for _, c := range r.Cards {
			if c.Kind == domain.ItemEpisode && (c.Show == nil || c.Show.Title != "The Wire" || c.DurationMS != time.Hour.Milliseconds()) {
				t.Errorf("%s: episode card %+v, want its show and its length", r.Kind, c)
			}
		}
	}

	// The profile's own arrangement: shows first, films hidden, the rest as they were.
	prefs := domain.DefaultPreferences()
	prefs.Home = []domain.HomeSection{
		{Row: domain.RowRecentShows, Visibility: domain.RowShown}, {Row: domain.RowRecentFilms, Visibility: domain.RowHidden},
	}
	if _, err := s.SetPreferences(ctx, profile.ID, prefs); err != nil {
		t.Fatal(err)
	}
	rows, err = s.Home(ctx, profile.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	var order []domain.HomeRow
	for _, r := range rows {
		order = append(order, r.Kind)
	}
	if want := []domain.HomeRow{domain.RowRecentShows, domain.RowNextUp}; !slices.Equal(order, want) {
		t.Errorf("arranged home = %v, want %v", order, want)
	}
	kept, err := s.Preferences(ctx, profile.ID)
	if err != nil || len(kept.Home) != len(domain.HomeRows()) || kept.Home[1] != prefs.Home[1] {
		t.Errorf("kept home = %+v, %v; want every row, as arranged", kept.Home, err)
	}
}

func TestNextEpisode(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	tv, err := s.AddLibrary(ctx, "TV", domain.LibraryShows, "/srv/tv")
	if err != nil {
		t.Fatal(err)
	}
	films, err := s.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	profile, err := s.AddProfile(ctx, "Oliver", domain.RoleAdmin, "hash")
	if err != nil {
		t.Fatal(err)
	}
	var eps []Episode
	for _, se := range [][2]int{{0, 1}, {1, 1}, {1, 2}, {1, 3}, {2, 1}} {
		rel := fmt.Sprintf("Wire/S%dE%d.mkv", se[0], se[1])
		eps = append(eps, Episode{
			Season: se[0], Episodes: []int{se[1]}, Title: fmt.Sprintf("S%dE%d", se[0], se[1]), Folder: "Wire", ByNumber: true,
			Copies: []Copy{{ContentKey: []byte(rel), Parts: []Part{{RelPath: rel, Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{Duration: time.Hour}}}}},
		})
	}
	if _, err := s.SaveShowFolder(ctx, tv.ID, "Wire", []byte("v"), Show{Title: "The Wire", Folder: "Wire"}, eps, nil); err != nil {
		t.Fatal(err)
	}
	title := func(kind domain.ItemKind, season, n int) uuid.UUID {
		t.Helper()
		return oneItem(t, s, `kind = $1 AND ($1 = 'show' OR season_number = $2) AND ($1 <> 'episode' OR episode_number = $3)`,
			kind, season, n).ID
	}
	show, season1 := title(domain.ItemShow, 0, 0), title(domain.ItemSeason, 1, 0)
	next := func(id uuid.UUID) string {
		t.Helper()
		c, err := s.Next(ctx, profile.ID, id)
		if errors.Is(err, ErrNoNext) {
			return "none"
		}
		if err != nil {
			t.Fatal(err)
		}
		return c.Title
	}
	for _, tc := range []struct {
		id   uuid.UUID
		want string
	}{
		{title(domain.ItemEpisode, 1, 1), "S1E2"},
		{title(domain.ItemEpisode, 1, 3), "S2E1"},
		{title(domain.ItemEpisode, 0, 1), "S1E1"},
		{title(domain.ItemEpisode, 2, 1), "none"},
		{show, "S1E1"},
	} {
		if got := next(tc.id); got != tc.want {
			t.Errorf("after %v: %s, want %s", tc.id, got, tc.want)
		}
	}

	if err := s.MarkWatched(ctx, profile.ID, title(domain.ItemEpisode, 1, 2), nil); err != nil {
		t.Fatal(err)
	}
	if got := next(show); got != "S1E3" {
		t.Errorf("a show with S1E2 watched starts at %s, want the one after it", got)
	}
	if _, err := s.SaveProgress(ctx, profile.ID, title(domain.ItemEpisode, 2, 1), 20*time.Minute, domain.ReachStart, nil); err != nil {
		t.Fatal(err)
	}
	if got := next(show); got != "S2E1" {
		t.Errorf("a show with an episode under way starts at %s, want that one", got)
	}
	if got := next(season1); got != "S1E3" {
		t.Errorf("season 1 starts at %s, want the one after its last watched", got)
	}
	if err := s.MarkWatched(ctx, profile.ID, show, nil); err != nil {
		t.Fatal(err)
	}
	if got := next(show); got != "S1E1" {
		t.Errorf("a finished show starts at %s, want its first episode", got)
	}

	kid, err := s.AddProfile(ctx, "Kid", domain.RoleRestricted, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetAccess(ctx, kid.ID, ProfileAccess{Libraries: []uuid.UUID{films.ID}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Next(ctx, kid.ID, show); !errors.Is(err, ErrNotFound) {
		t.Errorf("a show the profile may not see: %v, want ErrNotFound", err)
	}
}

func TestNextUpGoesOnFromTheFurthestEpisodeWatched(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	tv, err := s.AddLibrary(ctx, "TV", domain.LibraryShows, "/srv/tv")
	if err != nil {
		t.Fatal(err)
	}
	profile, err := s.AddProfile(ctx, "Oliver", domain.RoleAdmin, "hash")
	if err != nil {
		t.Fatal(err)
	}
	var eps []Episode
	for _, se := range [][2]int{{1, 1}, {1, 2}, {1, 3}, {2, 1}} {
		rel := fmt.Sprintf("Wire/S%dE%d.mkv", se[0], se[1])
		eps = append(eps, Episode{
			Season: se[0], Episodes: []int{se[1]}, Title: fmt.Sprintf("S%dE%d", se[0], se[1]), Folder: "Wire", ByNumber: true,
			Copies: []Copy{{ContentKey: []byte(rel), Parts: []Part{{RelPath: rel, Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{Duration: time.Hour}}}}},
		})
	}
	if _, err := s.SaveShowFolder(ctx, tv.ID, "Wire", []byte("v"), Show{Title: "The Wire", Folder: "Wire"}, eps, nil); err != nil {
		t.Fatal(err)
	}
	episode := func(season, n int) uuid.UUID {
		t.Helper()
		return oneItem(t, s, `kind = 'episode' AND season_number = $1 AND episode_number = $2`, season, n).ID
	}
	show := oneItem(t, s, `kind = 'show'`).ID
	nextUp := func() []string {
		t.Helper()
		rows, err := s.Home(ctx, profile.ID, 10)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, r := range rows {
			if r.Kind == domain.RowNextUp {
				for _, c := range r.Cards {
					out = append(out, c.Title)
				}
			}
		}
		return out
	}

	// S1E2 skipped, then S1E1 watched again after S1E3.
	for _, e := range []uuid.UUID{episode(1, 1), episode(1, 3), episode(1, 1)} {
		if err := s.MarkWatched(ctx, profile.ID, e, nil); err != nil {
			t.Fatal(err)
		}
	}
	if got := nextUp(); len(got) != 1 || got[0] != "S2E1" {
		t.Errorf("next up = %v, want S2E1, after the furthest episode watched", got)
	}
	if c, err := s.Next(ctx, profile.ID, show); err != nil || c.Title != "S2E1" {
		t.Errorf("the show's next = %q, %v; want S2E1", c.Title, err)
	}

	before, err := s.Title(ctx, profile.ID, episode(1, 3))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveProgress(ctx, profile.ID, episode(1, 3), time.Minute, domain.ReachStart, nil); err != nil {
		t.Fatal(err)
	}
	after, err := s.Title(ctx, profile.ID, episode(1, 3))
	if err != nil {
		t.Fatal(err)
	}
	if !after.State.LastPlayedAt.Equal(*before.State.LastPlayedAt) {
		t.Errorf("a peek at the start moved last played from %v to %v, want it left", before.State.LastPlayedAt, after.State.LastPlayedAt)
	}
}

// homeRow answers the titles on one of a profile's home rows, in order.
func homeRow(t *testing.T, s *Store, profile uuid.UUID, row domain.HomeRow) []string {
	t.Helper()
	rows, err := s.Home(t.Context(), profile, 10)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, r := range rows {
		if r.Kind == row {
			for _, c := range r.Cards {
				out = append(out, c.Title)
			}
		}
	}
	return out
}

// homeLibraries adds a films library holding films and a shows library holding shows of one
// episode each, and an admin and a profile allowed only the films.
func homeLibraries(t *testing.T, s *Store, films, shows []string) (admin, kid uuid.UUID) {
	t.Helper()
	ctx := t.Context()
	filmLib, err := s.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	tv, err := s.AddLibrary(ctx, "TV", domain.LibraryShows, "/srv/tv")
	if err != nil {
		t.Fatal(err)
	}
	part := func(rel string) []Copy {
		return []Copy{{ContentKey: []byte(rel), Parts: []Part{{RelPath: rel, Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{Duration: time.Hour}}}}}
	}
	for _, f := range films {
		if _, err := s.SaveFolder(ctx, filmLib.ID, f, []byte("v"), []Film{{Title: f, Folder: f, Copies: part(f + "/" + f + ".mkv")}}, nil); err != nil {
			t.Fatal(err)
		}
	}
	for _, sh := range shows {
		ep := Episode{Season: 1, Episodes: []int{1}, Title: sh + " S1E1", Folder: sh, ByNumber: true, Copies: part(sh + "/S1E1.mkv")}
		if _, err := s.SaveShowFolder(ctx, tv.ID, sh, []byte("v"), Show{Title: sh, Folder: sh}, []Episode{ep}, nil); err != nil {
			t.Fatal(err)
		}
	}
	a, err := s.AddProfile(ctx, "Oliver", domain.RoleAdmin, "hash")
	if err != nil {
		t.Fatal(err)
	}
	k, err := s.AddProfile(ctx, "Kid", domain.RoleRestricted, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetAccess(ctx, k.ID, ProfileAccess{Libraries: []uuid.UUID{filmLib.ID}}); err != nil {
		t.Fatal(err)
	}
	return a.ID, k.ID
}

func TestRecentlyReleased(t *testing.T) {
	s := migrated(t)
	admin, kid := homeLibraries(t, s, []string{"Weeks", "Months", "Long Ago", "To Come", "Undated"}, []string{"The Wire"})
	for title, daysAgo := range map[string]int{"Weeks": 10, "Months": 100, "Long Ago": 800, "To Come": -30, "The Wire S1E1": 30} {
		if _, err := s.pool.Exec(t.Context(), `UPDATE items SET release_date = current_date - $2::int WHERE title = $1`, title, daysAgo); err != nil {
			t.Fatal(err)
		}
	}

	if got, want := homeRow(t, s, admin, domain.RowRecentlyReleased), []string{"Weeks", "The Wire", "Months"}; !slices.Equal(got, want) {
		t.Errorf("recently released = %v, want %v: the newest first, a show by its episode, nothing old or yet to come", got, want)
	}
	if got, want := homeRow(t, s, kid, domain.RowRecentlyReleased), []string{"Weeks", "Months"}; !slices.Equal(got, want) {
		t.Errorf("recently released for a profile without the shows = %v, want %v", got, want)
	}
}

func TestTopRatedUnwatched(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	admin, kid := homeLibraries(t, s, []string{"Best", "Few Votes", "Watched", "Started", "Other Site", "Good"}, []string{"Begun", "Fresh"})
	for _, r := range []struct {
		title, source, site string
		score               float32
		votes               *int
	}{
		{"Best", "tmdb", "imdb", 40, nil},
		{"Best", "omdb", "imdb", 90, new(250000)},
		{"Few Votes", "omdb", "imdb", 95, new(12)},
		{"Watched", "omdb", "imdb", 88, nil},
		{"Started", "omdb", "imdb", 87, nil},
		{"Other Site", "tmdb", "tmdb", 99, nil},
		{"Begun", "tmdb", "imdb", 86, nil},
		{"Fresh", "tmdb", "imdb", 75, nil},
		{"Good", "tmdb", "imdb", 65, nil},
	} {
		if _, err := s.pool.Exec(ctx, `INSERT INTO ratings (item_id, source, site, score, votes)
			SELECT id, $2, $3, $4, $5 FROM items WHERE title = $1`, r.title, r.source, r.site, r.score, r.votes); err != nil {
			t.Fatal(err)
		}
	}
	titled := func(title string) uuid.UUID { return oneItem(t, s, `title = $1`, title).ID }
	if err := s.MarkWatched(ctx, admin, titled("Watched"), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveProgress(ctx, admin, titled("Started"), 20*time.Minute, domain.ReachStart, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkWatched(ctx, admin, titled("Begun S1E1"), nil); err != nil {
		t.Fatal(err)
	}

	if got, want := homeRow(t, s, admin, domain.RowTopRatedUnwatched), []string{"Best", "Fresh", "Good"}; !slices.Equal(got, want) {
		t.Errorf("top rated = %v, want %v: by IMDb, nothing begun or rated by too few", got, want)
	}
	if got, want := homeRow(t, s, kid, domain.RowTopRatedUnwatched), []string{"Best", "Watched", "Started", "Good"}; !slices.Equal(got, want) {
		t.Errorf("top rated for another profile, without the shows = %v, want %v", got, want)
	}
}

// The watchlist holds films and shows, the latest put on it first, and lets go of a film once it is
// watched and a show once every episode is.
func TestWatchlist(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	admin, kid := homeLibraries(t, s, []string{"Heat", "Alien"}, []string{"The Wire"})
	show := oneItem(t, s, `kind = 'show'`)
	var eps []Episode
	for _, n := range []int{1, 2} {
		rel := fmt.Sprintf("The Wire/S1E%d.mkv", n)
		eps = append(eps, Episode{Season: 1, Episodes: []int{n}, Title: rel, Folder: "The Wire", ByNumber: true, Copies: []Copy{{
			ContentKey: []byte(rel), Parts: []Part{{RelPath: rel, Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{Duration: time.Hour}}},
		}}})
	}
	if _, err := s.SaveShowFolder(ctx, show.LibraryID, "The Wire", []byte("v2"), Show{Title: "The Wire", Folder: "The Wire"}, eps, nil); err != nil {
		t.Fatal(err)
	}
	heat, alien := oneItem(t, s, `title = 'Heat'`).ID, oneItem(t, s, `title = 'Alien'`).ID
	first, second := oneItem(t, s, `kind = 'episode' AND episode_number = 1`).ID, oneItem(t, s, `kind = 'episode' AND episode_number = 2`).ID
	for _, id := range []uuid.UUID{heat, first, alien} {
		if err := s.Watchlist(ctx, admin, id); err != nil {
			t.Fatal(err)
		}
	}
	if got, want := homeRow(t, s, admin, domain.RowWatchlist), []string{"Alien", "The Wire", "Heat"}; !slices.Equal(got, want) {
		t.Errorf("watchlist = %v, want %v: an episode puts its show on it", got, want)
	}
	if got := homeRow(t, s, kid, domain.RowWatchlist); len(got) != 0 {
		t.Errorf("another profile's watchlist = %v, want nothing", got)
	}
	if page, err := s.Title(ctx, admin, show.ID); err != nil || page.State.WatchlistedAt == nil {
		t.Errorf("the show's state = %+v, %v; want it on the watchlist", page.State, err)
	}
	box, err := s.AddCollection(ctx, show.LibraryID, "Box")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Watchlist(ctx, admin, box); !errors.Is(err, ErrNotListable) {
		t.Errorf("a collection put on the watchlist: %v, want %v", err, ErrNotListable)
	}

	if err := s.MarkWatched(ctx, admin, heat, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveProgress(ctx, admin, first, time.Hour, domain.ReachResumable, nil); err != nil {
		t.Fatal(err)
	}
	if got, want := homeRow(t, s, admin, domain.RowWatchlist), []string{"Alien", "The Wire"}; !slices.Equal(got, want) {
		t.Errorf("after watching Heat and an episode, watchlist = %v, want %v", got, want)
	}
	if err := s.MarkWatched(ctx, admin, second, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Unwatchlist(ctx, admin, alien); err != nil {
		t.Fatal(err)
	}
	if got := homeRow(t, s, admin, domain.RowWatchlist); len(got) != 0 {
		t.Errorf("after watching the show through and taking Alien off, watchlist = %v, want nothing", got)
	}
}

// The watchlist is paged across every library the profile sees, the latest put on it first.
func TestWatchlistPage(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	admin, kid := homeLibraries(t, s, []string{"Heat", "Alien"}, []string{"The Wire"})
	heat, alien, show := oneItem(t, s, `title = 'Heat'`).ID, oneItem(t, s, `title = 'Alien'`).ID, oneItem(t, s, `kind = 'show'`).ID
	for _, profile := range []uuid.UUID{admin, kid} {
		for _, id := range []uuid.UUID{heat, show, alien} {
			if err := s.Watchlist(ctx, profile, id); err != nil {
				t.Fatal(err)
			}
		}
	}
	page := func(profile uuid.UUID, offset, limit int) ([]string, int64) {
		t.Helper()
		cards, total, err := s.WatchlistPage(ctx, profile, offset, limit)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, c := range cards {
			out = append(out, c.Title)
		}
		return out, total
	}
	for _, tc := range []struct {
		name          string
		profile       uuid.UUID
		offset, limit int
		want          []string
		total         int64
	}{
		{"everything", admin, 0, 10, []string{"Alien", "The Wire", "Heat"}, 3},
		{"the second page of one", admin, 1, 1, []string{"The Wire"}, 3},
		{"a profile that does not see the show", kid, 0, 10, []string{"Alien", "Heat"}, 2},
	} {
		if got, total := page(tc.profile, tc.offset, tc.limit); !slices.Equal(got, tc.want) || total != tc.total {
			t.Errorf("%s: %v of %d, want %v of %d", tc.name, got, total, tc.want, tc.total)
		}
	}
	if err := s.Unwatchlist(ctx, admin, show); err != nil {
		t.Fatal(err)
	}
	if got, total := page(admin, 0, 10); !slices.Equal(got, []string{"Alien", "Heat"}) || total != 2 {
		t.Errorf("after taking the show off: %v of %d, want Alien and Heat", got, total)
	}
}
