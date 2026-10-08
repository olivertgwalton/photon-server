//go:build integration

package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
)

// saveProgress saves progress as a player's report does: against the title's length.
func saveProgress(ctx context.Context, s *Store, profile, item uuid.UUID, position time.Duration, before domain.Reach, at *time.Time) (domain.Reach, error) {
	length, err := s.Length(ctx, item)
	if err != nil {
		return "", err
	}
	return s.SaveProgress(ctx, profile, item, position, length, before, at)
}

func TestWhatAProfileHasWatched(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "TV", domain.LibraryShows, "/srv/tv")
	if err != nil {
		t.Fatal(err)
	}
	oliver, err := s.AddProfile(ctx, "Oliver", domain.RoleAdmin, "hash")
	if err != nil {
		t.Fatal(err)
	}
	guest, err := s.AddProfile(ctx, "Guest", domain.RoleMember, "hash")
	if err != nil {
		t.Fatal(err)
	}
	var episodes []Episode
	for n := 1; n <= 3; n++ {
		episodes = append(episodes, Episode{
			Season: 1, Episodes: []int{n}, Title: "episode", Folder: "The Wire/Season 1", ByNumber: true,
			Copies: []Copy{{ContentKey: []byte{byte(n)}, Parts: []Part{{
				RelPath: "The Wire/Season 1/" + string(rune('0'+n)) + ".mkv", Size: 1, ModTime: time.Unix(0, 0),
				Facts: &domain.Facts{Duration: time.Hour},
			}}}},
		})
	}
	if _, err := s.SaveShowFolder(ctx, lib.ID, "The Wire/Season 1", []byte("v1"), Show{Title: "the wire", Folder: "The Wire"}, episodes, nil); err != nil {
		t.Fatal(err)
	}
	var show uuid.UUID
	if err := s.pool.QueryRow(ctx, `SELECT id FROM items WHERE kind = 'show'`).Scan(&show); err != nil {
		t.Fatal(err)
	}
	page := func(profile uuid.UUID) TitlePage {
		t.Helper()
		p, err := s.Title(ctx, profile, show)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	season := func(profile uuid.UUID) TitlePage {
		t.Helper()
		p, err := s.Title(ctx, profile, page(profile).Seasons[0].ID)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	first, second := season(oliver.ID).Episodes[0].ID, season(oliver.ID).Episodes[1].ID

	// In order: a stop near the start puts the position back to nothing.
	for _, p := range []struct {
		at   time.Duration
		want domain.Reach
	}{{2 * time.Minute, domain.ReachStart}, {20 * time.Minute, domain.ReachResumable}} {
		if reach, err := saveProgress(ctx, s, oliver.ID, second, p.at, domain.ReachStart, nil); err != nil || reach != p.want {
			t.Errorf("progress at %v: %s, %v; want %s", p.at, reach, err, p.want)
		}
	}
	if reach, err := saveProgress(ctx, s, oliver.ID, first, 58*time.Minute, domain.ReachStart, nil); err != nil || reach != domain.ReachEnd {
		t.Errorf("progress near the end: %s, %v; want it watched", reach, err)
	}
	eps := season(oliver.ID).Episodes
	if eps[0].State.WatchedAt == nil || eps[0].State.Plays != 1 || eps[1].State.PositionMS != (20*time.Minute).Milliseconds() {
		t.Errorf("episodes = %+v, %+v; want the first watched, the second resumable at 20 minutes", eps[0].State, eps[1].State)
	}
	if st := page(oliver.ID).State; st.Unwatched != 2 || st.WatchedAt != nil || st.LastPlayedAt == nil {
		t.Errorf("show = %+v, want two left, not watched, last played", st)
	}
	if st := page(guest.ID).State; st.Unwatched != 3 || st.LastPlayedAt != nil {
		t.Errorf("another profile's show = %+v, want all three left", st)
	}

	if err := s.MarkWatched(ctx, oliver.ID, show, nil); err != nil {
		t.Fatal(err)
	}
	if st := page(oliver.ID).State; st.Unwatched != 0 || st.WatchedAt == nil {
		t.Errorf("after marking the show, it = %+v, want every episode watched", st)
	}
	if err := s.MarkUnwatched(ctx, oliver.ID, page(oliver.ID).Seasons[0].ID); err != nil {
		t.Fatal(err)
	}
	if st := season(oliver.ID).Episodes[1].State; st.WatchedAt != nil || st.PositionMS != 0 || st.Plays != 1 {
		t.Errorf("after unmarking the season, an episode = %+v, want unwatched from the start, its play still counted", st)
	}

	if err := s.Favourite(ctx, oliver.ID, show); err != nil {
		t.Fatal(err)
	}
	cards, _, err := s.Wall(ctx, []uuid.UUID{lib.ID}, WallPage{Profile: oliver.ID, Sort: domain.SortTitle, Order: domain.Ascending, Limit: 5})
	if err != nil || len(cards) != 1 || cards[0].State.FavouriteAt == nil || cards[0].State.Unwatched != 3 {
		t.Errorf("card = %+v, %v; want a favourite with three left", cards, err)
	}
	if err := s.Unfavourite(ctx, oliver.ID, show); err != nil {
		t.Fatal(err)
	}
	if page(oliver.ID).State.FavouriteAt != nil {
		t.Error("still a favourite after unfavouriting")
	}
	if _, err := saveProgress(ctx, s, oliver.ID, uuid.NewV7(), time.Minute, domain.ReachStart, nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("progress on no title: %v, want ErrNotFound", err)
	}
}

func TestAPlaybackIsOnePlayHoweverOftenItReportsTheEnd(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	oliver, err := s.AddProfile(ctx, "Oliver", domain.RoleAdmin, "hash")
	if err != nil {
		t.Fatal(err)
	}
	heat := []Copy{{ContentKey: []byte("heat"), Parts: []Part{{
		RelPath: "Heat/Heat.mkv", Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{Duration: time.Hour},
	}}}}
	if _, err := s.SaveFolder(ctx, lib.ID, "Heat", []byte("v"), []Film{{Title: "Heat", Folder: "Heat", Copies: heat}}, nil); err != nil {
		t.Fatal(err)
	}
	var film uuid.UUID
	if err := s.pool.QueryRow(ctx, `SELECT id FROM items WHERE kind = 'movie'`).Scan(&film); err != nil {
		t.Fatal(err)
	}
	state := func() TitleState {
		t.Helper()
		p, err := s.Title(ctx, oliver.ID, film)
		if err != nil {
			t.Fatal(err)
		}
		return p.State
	}

	// A player reports every ten seconds through the last tenth, then stops at the end: its
	// playback has reached the end since the first of those reports.
	before := domain.ReachResumable
	for at := 55 * time.Minute; at <= time.Hour; at += 10 * time.Second {
		reach, err := saveProgress(ctx, s, oliver.ID, film, at, before, nil)
		if err != nil || reach != domain.ReachEnd {
			t.Fatalf("progress at %v: %s, %v; want the end", at, reach, err)
		}
		before = reach
	}
	first := state()
	if first.Plays != 1 || first.WatchedAt == nil || first.PositionMS != 0 {
		t.Fatalf("after one playback = %+v, want one play, watched, no position", first)
	}

	if err := s.MarkWatched(ctx, oliver.ID, film, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := saveProgress(ctx, s, oliver.ID, film, time.Hour, domain.ReachResumable, nil); err != nil {
		t.Fatal(err)
	}
	if again := state(); again.Plays != 2 || !again.WatchedAt.Equal(*first.WatchedAt) {
		t.Errorf("after marking it and watching it again = %+v, want a second play and the first watched time %v", again, first.WatchedAt)
	}
}

// Progress and marks watched offline and sent later land at when they happened, and change nothing
// that has changed since, whichever of them came after.
func TestTheNewestWatchWins(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	oliver, err := s.AddProfile(ctx, "Oliver", domain.RoleAdmin, "hash")
	if err != nil {
		t.Fatal(err)
	}
	heat := []Copy{{ContentKey: []byte("heat"), Parts: []Part{{
		RelPath: "Heat/Heat.mkv", Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{Duration: time.Hour},
	}}}}
	if _, err := s.SaveFolder(ctx, lib.ID, "Heat", []byte("v"), []Film{{Title: "Heat", Folder: "Heat", Copies: heat}}, nil); err != nil {
		t.Fatal(err)
	}
	var film uuid.UUID
	if err := s.pool.QueryRow(ctx, `SELECT id FROM items WHERE kind = 'movie'`).Scan(&film); err != nil {
		t.Fatal(err)
	}
	state := func() TitleState {
		t.Helper()
		p, err := s.Title(ctx, oliver.ID, film)
		if err != nil {
			t.Fatal(err)
		}
		return p.State
	}
	ago := func(d time.Duration) *time.Time { return new(time.Now().Add(-d).Truncate(time.Second)) }
	yesterday, twoDays, anHour := ago(24*time.Hour), ago(48*time.Hour), ago(time.Hour)

	if err := s.MarkWatched(ctx, oliver.ID, film, yesterday); err != nil {
		t.Fatal(err)
	}
	if st := state(); st.WatchedAt == nil || !st.WatchedAt.Equal(*yesterday) || !st.LastPlayedAt.Equal(*yesterday) {
		t.Fatalf("marked watched yesterday = %+v, want watched and last played yesterday", st)
	}
	if _, err := saveProgress(ctx, s, oliver.ID, film, 20*time.Minute, domain.ReachStart, twoDays); !errors.Is(err, ErrSuperseded) {
		t.Errorf("progress from before it was marked watched: %v, want superseded", err)
	}
	if st := state(); st.PositionMS != 0 || st.WatchedAt == nil {
		t.Errorf("after progress from before it was watched = %+v, want it watched with no position", st)
	}

	if _, err := saveProgress(ctx, s, oliver.ID, film, 20*time.Minute, domain.ReachStart, anHour); err != nil {
		t.Fatal(err)
	}
	if _, err := saveProgress(ctx, s, oliver.ID, film, 40*time.Minute, domain.ReachStart, ago(2*time.Hour)); !errors.Is(err, ErrSuperseded) {
		t.Errorf("older progress: %v, want superseded", err)
	}
	if st := state(); st.PositionMS != (20*time.Minute).Milliseconds() || !st.LastPlayedAt.Equal(*anHour) {
		t.Errorf("after older progress = %+v, want 20 minutes in, last played an hour ago", st)
	}

	if err := s.MarkUnwatched(ctx, oliver.ID, film); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkWatched(ctx, oliver.ID, film, ago(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if st := state(); st.WatchedAt != nil {
		t.Errorf("marked watched from before it was marked unwatched = %+v, want it unwatched", st)
	}

	// State kept from before changes were timed, last played an hour ago.
	if _, err := s.pool.Exec(ctx, `UPDATE watch_state SET changed_at = NULL, last_played_at = $1`, anHour); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkWatched(ctx, oliver.ID, film, yesterday); err != nil {
		t.Fatal(err)
	}
	if st := state(); st.WatchedAt != nil || !st.LastPlayedAt.Equal(*anHour) {
		t.Errorf("marked watched yesterday over state last played an hour ago = %+v, want it unwatched, last played an hour ago", st)
	}
}

// Victorious in a Kids library and a Shows library, as symlinks to the same files, is one show:
// one search result and one card on home, its state the same in both, and each library still
// listing its own.
func TestATitleInTwoLibrariesIsOneTitle(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	kids, err := s.AddLibrary(ctx, "Kids", domain.LibraryShows, "/srv/kids/shows")
	if err != nil {
		t.Fatal(err)
	}
	shows, err := s.AddLibrary(ctx, "Shows", domain.LibraryShows, "/srv/shows")
	if err != nil {
		t.Fatal(err)
	}
	oliver, err := s.AddProfile(ctx, "Oliver", domain.RoleAdmin, "hash")
	if err != nil {
		t.Fatal(err)
	}
	sam, err := s.AddProfile(ctx, "Sam", domain.RoleMember, "hash")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetAccess(ctx, sam.ID, ProfileAccess{Libraries: []uuid.UUID{shows.ID}}); err != nil {
		t.Fatal(err)
	}
	// add scans and matches the show into a library, answering its episodes by number.
	add := func(lib uuid.UUID) (uuid.UUID, map[int]uuid.UUID) {
		t.Helper()
		var eps []Episode
		for n := 1; n <= 3; n++ {
			rel := fmt.Sprintf("Victorious/S01E0%d.mkv", n)
			eps = append(eps, Episode{Season: 1, Episodes: []int{n}, Title: fmt.Sprintf("Episode %d", n), Folder: "Victorious", ByNumber: true, Copies: []Copy{{
				ContentKey: []byte(rel), Parts: []Part{{RelPath: rel, Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{Duration: time.Hour}}},
			}}})
		}
		saved, err := s.SaveShowFolder(ctx, lib, "Victorious", []byte("v"), Show{Title: "Victorious", Folder: "Victorious"}, eps, nil)
		if err != nil {
			t.Fatal(err)
		}
		var show uuid.UUID
		if err := s.pool.QueryRow(ctx, `SELECT id FROM items WHERE library_id = $1 AND kind = 'show'`, lib).Scan(&show); err != nil {
			t.Fatal(err)
		}
		if err := s.SaveIdentity(ctx, show, domain.SourceTMDB, domain.Metadata{Title: "Victorious", IDs: map[domain.Provider]string{domain.ProviderTMDB: "36685"}}, nil); err != nil {
			t.Fatal(err)
		}
		episodes := map[int]uuid.UUID{}
		for _, id := range saved.Titles[domain.TitleAdded] {
			var number int
			if s.pool.QueryRow(ctx, `SELECT episode_number FROM items WHERE id = $1 AND kind = 'episode'`, id).Scan(&number) == nil {
				episodes[number] = id
			}
		}
		return show, episodes
	}
	watched := func(profile, id uuid.UUID) bool {
		t.Helper()
		row, err := readItem(ctx, s.pool, id)
		if err != nil {
			t.Fatal(err)
		}
		states, err := s.states(ctx, profile, []*model.Item{row})
		if err != nil {
			t.Fatal(err)
		}
		return states[row.ID].WatchedAt != nil
	}
	cards := func(profile uuid.UUID) (search []uuid.UUID, home map[domain.HomeRow][]uuid.UUID) {
		t.Helper()
		found, _, err := s.Search(ctx, SearchQuery{Profile: profile, Text: "victorious", Limit: 10})
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range found {
			search = append(search, c.ID)
		}
		rows, err := s.Home(ctx, profile, 10)
		if err != nil {
			t.Fatal(err)
		}
		home = map[domain.HomeRow][]uuid.UUID{}
		for _, r := range rows {
			for _, c := range r.Cards {
				home[r.Kind] = append(home[r.Kind], c.ID)
			}
		}
		return search, home
	}

	// Watched in Shows before Kids held it, it is watched in Kids once Kids has it.
	inShows, showsEps := add(shows.ID)
	if err := s.MarkWatched(ctx, oliver.ID, showsEps[1], nil); err != nil {
		t.Fatal(err)
	}
	inKids, kidsEps := add(kids.ID)
	if !watched(oliver.ID, kidsEps[1]) {
		t.Error("an episode watched in one library is unwatched where another library gained it")
	}

	// Kids was added first, so it is the copy shown to whoever sees both, but in each library's own
	// recently added row, as Plex's.
	search, home := cards(oliver.ID)
	if !slices.Equal(search, []uuid.UUID{inKids}) || !slices.Equal(home[domain.RowRecentShows], []uuid.UUID{inKids, inShows}) ||
		!slices.Equal(home[domain.RowNextUp], []uuid.UUID{kidsEps[2]}) {
		t.Errorf("search = %v, recently added = %v, next up = %v; want Kids' show once, each library's in its row, and its second episode",
			search, home[domain.RowRecentShows], home[domain.RowNextUp])
	}
	if same, err := s.SameTitles(ctx, oliver.ID, inShows); err != nil || len(same) != 2 || !slices.Contains(same, inKids) {
		t.Errorf("Shows' show is the same as %v (%v), want it and Kids'", same, err)
	}
	if same, err := s.SameTitles(ctx, sam.ID, inKids); err != nil || !slices.Equal(same, []uuid.UUID{inShows}) {
		t.Errorf("Kids' show is the same as %v (%v) for a profile seeing only Shows, want Shows'", same, err)
	}
	// Sam sees only Shows, so Shows' copy.
	if search, home := cards(sam.ID); !slices.Equal(search, []uuid.UUID{inShows}) || !slices.Equal(home[domain.RowRecentShows], []uuid.UUID{inShows}) {
		t.Errorf("a profile seeing only Shows is shown search %v and recently added %v, want Shows' show", search, home[domain.RowRecentShows])
	}

	// Under way in Kids is under way in Shows, and once on home.
	if _, err := saveProgress(ctx, s, oliver.ID, kidsEps[2], 20*time.Minute, domain.ReachStart, nil); err != nil {
		t.Fatal(err)
	}
	if _, home := cards(oliver.ID); !slices.Equal(home[domain.RowContinueWatching], []uuid.UUID{kidsEps[2]}) {
		t.Errorf("continue watching = %v, want Kids' second episode once", home[domain.RowContinueWatching])
	}
	if next, err := s.Next(ctx, oliver.ID, inShows); err != nil || next.ID != showsEps[2] {
		t.Errorf("Shows' show resumes at %v (%v), want its second episode, under way in Kids", next.ID, err)
	}
	// Unwatched in Kids is unwatched in Shows.
	if err := s.MarkUnwatched(ctx, oliver.ID, kidsEps[1]); err != nil {
		t.Fatal(err)
	}
	if watched(oliver.ID, showsEps[1]) {
		t.Error("an episode unwatched in one library stays watched in the other")
	}

	for _, lib := range []uuid.UUID{kids.ID, shows.ID} {
		if wall, _, err := s.Wall(ctx, []uuid.UUID{lib}, WallPage{Profile: oliver.ID, Sort: domain.SortTitle, Limit: 10}); err != nil || len(wall) != 1 {
			t.Errorf("library %v lists %d shows (%v), want its own", lib, len(wall), err)
		}
	}
}

// A show TMDB matched in one library and TheTVDB alone in another, from files of their own, is one
// show once TMDB has given its TVDB id, as it gives every show's.
func TestATitleMatchedByDifferentProvidersIsOneTitle(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	oliver, err := s.AddProfile(ctx, "Oliver", domain.RoleAdmin, "hash")
	if err != nil {
		t.Fatal(err)
	}
	sam, err := s.AddProfile(ctx, "Sam", domain.RoleMember, "hash")
	if err != nil {
		t.Fatal(err)
	}
	add := func(name string, source domain.FieldSource, ids map[domain.Provider]string) (uuid.UUID, uuid.UUID, uuid.UUID) {
		t.Helper()
		lib, err := s.AddLibrary(ctx, name, domain.LibraryShows, "/srv/"+name)
		if err != nil {
			t.Fatal(err)
		}
		rel := "Victorious/S01E01.mkv"
		saved, err := s.SaveShowFolder(ctx, lib.ID, "Victorious", []byte("v"), Show{Title: "Victorious", Folder: "Victorious"}, []Episode{{
			Season: 1, Episodes: []int{1}, Title: "Pilot", Folder: "Victorious", ByNumber: true, Copies: []Copy{{
				ContentKey: []byte(name + rel), Parts: []Part{{RelPath: rel, Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{Duration: time.Hour}}},
			}},
		}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		var show uuid.UUID
		if err := s.pool.QueryRow(ctx, `SELECT id FROM items WHERE library_id = $1 AND kind = 'show'`, lib.ID).Scan(&show); err != nil {
			t.Fatal(err)
		}
		if err := s.SaveIdentity(ctx, show, source, domain.Metadata{Title: "Victorious", IDs: ids}, nil); err != nil {
			t.Fatal(err)
		}
		var episode uuid.UUID
		for _, id := range saved.Titles[domain.TitleAdded] {
			if s.pool.QueryRow(ctx, `SELECT 1 FROM items WHERE id = $1 AND kind = 'episode'`, id).Scan(new(int)) == nil {
				episode = id
			}
		}
		return lib.ID, show, episode
	}
	_, inKids, kidsPilot := add("Kids", domain.SourceTMDB, map[domain.Provider]string{domain.ProviderTMDB: "36685", domain.ProviderTVDB: "175901"})
	shows, inShows, showsPilot := add("Shows", domain.SourceTVDB, map[domain.Provider]string{domain.ProviderTVDB: "175901"})
	if err := s.SetAccess(ctx, sam.ID, ProfileAccess{Libraries: []uuid.UUID{shows}}); err != nil {
		t.Fatal(err)
	}

	for profile, want := range map[uuid.UUID]uuid.UUID{oliver.ID: inKids, sam.ID: inShows} {
		found, total, err := s.Search(ctx, SearchQuery{Profile: profile, Text: "victorious", Limit: 10})
		if err != nil || total != 1 || len(found) != 1 || found[0].ID != want {
			t.Errorf("search for %v = %v of %d (%v), want only %v", profile, found, total, err, want)
		}
	}
	if err := s.MarkWatched(ctx, oliver.ID, showsPilot, nil); err != nil {
		t.Fatal(err)
	}
	row, err := readItem(ctx, s.pool, kidsPilot)
	if err != nil {
		t.Fatal(err)
	}
	if states, err := s.states(ctx, oliver.ID, []*model.Item{row}); err != nil || states[row.ID].WatchedAt == nil {
		t.Errorf("the pilot watched under Shows is %+v under Kids (%v), want watched", states[row.ID], err)
	}
}

// Titles are the same through one they each share an id with: a film TMDB and IMDb know, one IMDb
// and TVDB know, and one only TVDB knows are one film, until the one joining them goes.
func TestTitlesSharingAnIDThroughAnotherAreOneTitle(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	oliver, err := s.AddProfile(ctx, "Oliver", domain.RoleAdmin, "hash")
	if err != nil {
		t.Fatal(err)
	}
	add := func(name string, ids map[domain.Provider]string) (uuid.UUID, uuid.UUID) {
		t.Helper()
		lib, err := s.AddLibrary(ctx, name, domain.LibraryMovies, "/srv/"+name)
		if err != nil {
			t.Fatal(err)
		}
		rel := "Heat (1995)/Heat.mkv"
		if _, err := s.SaveFolder(ctx, lib.ID, "Heat (1995)", []byte("v"), []Film{{Title: "Heat", Year: 1995, Folder: "Heat (1995)", IDs: ids, Copies: []Copy{{
			ContentKey: []byte(name + rel), Parts: []Part{{RelPath: rel, Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{Duration: time.Hour}}},
		}}}}, nil); err != nil {
			t.Fatal(err)
		}
		var film uuid.UUID
		if err := s.pool.QueryRow(ctx, `SELECT id FROM items WHERE library_id = $1`, lib.ID).Scan(&film); err != nil {
			t.Fatal(err)
		}
		return lib.ID, film
	}
	_, a := add("A", map[domain.Provider]string{domain.ProviderTMDB: "949", domain.ProviderIMDb: "tt0113277"})
	_, c := add("C", map[domain.Provider]string{domain.ProviderTVDB: "1234"})
	if same, err := s.SameTitles(ctx, oliver.ID, c); err != nil || len(same) != 1 {
		t.Errorf("before anything joins them C is the same as %v (%v), want only itself", same, err)
	}
	b, _ := add("B", map[domain.Provider]string{domain.ProviderIMDb: "tt0113277", domain.ProviderTVDB: "1234"})
	if same, err := s.SameTitles(ctx, oliver.ID, c); err != nil || len(same) != 3 || !slices.Contains(same, a) {
		t.Errorf("C is the same as %v (%v), want A, B and itself", same, err)
	}
	if found, total, err := s.Search(ctx, SearchQuery{Profile: oliver.ID, Text: "heat", Limit: 10}); err != nil || total != 1 || found[0].ID != a {
		t.Errorf("search = %v of %d (%v), want A's film once", found, total, err)
	}

	if err := s.RemoveLibrary(ctx, b); err != nil {
		t.Fatal(err)
	}
	if same, err := s.SameTitles(ctx, oliver.ID, c); err != nil || len(same) != 1 {
		t.Errorf("with B gone C is the same as %v (%v), want only itself", same, err)
	}
	if _, total, err := s.Search(ctx, SearchQuery{Profile: oliver.ID, Text: "heat", Limit: 10}); err != nil || total != 2 {
		t.Errorf("with B gone search finds %d (%v), want A's and C's", total, err)
	}
}
