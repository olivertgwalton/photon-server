//go:build integration

package store

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

func TestCalendar(t *testing.T) {
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
	kidsTV, err := s.AddLibrary(ctx, "Kids TV", domain.LibraryShows, "/srv/kids")
	if err != nil {
		t.Fatal(err)
	}
	admin, err := s.AddProfile(ctx, "Oliver", domain.RoleAdmin, "hash")
	if err != nil {
		t.Fatal(err)
	}
	kid, err := s.AddProfile(ctx, "Kid", domain.RoleUser, "hash")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetAccess(ctx, kid.ID, ProfileAccess{Libraries: []uuid.UUID{films.ID, kidsTV.ID}}); err != nil {
		t.Fatal(err)
	}
	day := func(n int) time.Time { return time.Date(2026, time.October, n, 0, 0, 0, 0, time.UTC) }
	copies := func(rel string) []Copy {
		return []Copy{{ContentKey: []byte(rel), Parts: []Part{{RelPath: rel, Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{}}}}}
	}
	if _, err := s.SaveFolder(ctx, films.ID, "Heat", []byte("v"), []Film{{Title: "Heat", Folder: "Heat", Copies: copies("Heat/Heat.mkv")}}, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveIdentity(ctx, oneItem(t, s, "title = 'Heat'").ID, domain.SourceTMDB, domain.Metadata{ReleaseDate: day(3)}, nil); err != nil {
		t.Fatal(err)
	}
	saveShow := func(lib uuid.UUID, title string, episodes ...int) {
		t.Helper()
		var eps []Episode
		for _, n := range episodes {
			rel := fmt.Sprintf("%s/S01E%02d.mkv", title, n)
			eps = append(eps, Episode{Season: 1, Episodes: []int{n}, Title: rel, Folder: title, ByNumber: true, Copies: copies(rel)})
		}
		if _, err := s.SaveShowFolder(ctx, lib, title, fmt.Append(nil, episodes), Show{Title: title, Folder: title}, eps, nil); err != nil {
			t.Fatal(err)
		}
	}
	saveShow(tv.ID, "Severance", 1)
	severance := oneItem(t, s, "kind = 'show' AND title = 'Severance'").ID
	describe := func(show uuid.UUID, episodes map[int]domain.Metadata) {
		t.Helper()
		if err := s.SaveIdentity(ctx, show, domain.SourceTMDB, domain.Metadata{}, map[int]domain.SeasonMetadata{1: {Episodes: episodes}}); err != nil {
			t.Fatal(err)
		}
	}
	season := map[int]domain.Metadata{
		1: {Title: "Good News About Hell", ReleaseDate: day(10)},
		2: {Title: "Half Loop", ReleaseDate: day(17)},
		3: {Title: "In Perpetuity"},
	}
	describe(severance, season)
	saveShow(kidsTV.ID, "Bluey", 2)
	describe(oneItem(t, s, "kind = 'show' AND title = 'Bluey'").ID, map[int]domain.Metadata{1: {Title: "Magic Xylophone", ReleaseDate: day(17)}, 5: {Title: "Bike"}})

	calendar := func(profile uuid.UUID, filter domain.CalendarFilter) []string {
		t.Helper()
		days, err := s.Calendar(ctx, CalendarQuery{Profile: profile, Start: day(1), End: day(31), Filter: filter})
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, d := range days {
			for _, e := range d.Entries {
				entry := []string{d.Date.Format(time.DateOnly), string(e.Availability), e.Title}
				for _, m := range e.Milestones {
					entry = append(entry, string(m))
				}
				if e.State.WatchedAt != nil {
					entry = append(entry, "watched")
				}
				out = append(out, strings.Join(entry, " "))
			}
		}
		return out
	}

	want := []string{
		"2026-10-03 available Heat",
		"2026-10-10 available Good News About Hell series_premiere",
		"2026-10-17 announced Magic Xylophone series_premiere",
		"2026-10-17 announced Half Loop",
	}
	if got := calendar(admin.ID, domain.CalendarAll); !slices.Equal(got, want) {
		t.Errorf("calendar = %q, want %q: an episode a provider lists without a file is announced, one undated is not, though it is after", got, want)
	}
	if got, want := calendar(kid.ID, domain.CalendarAll), []string{"2026-10-03 available Heat", "2026-10-17 announced Magic Xylophone series_premiere"}; !slices.Equal(got, want) {
		t.Errorf("a restricted profile's calendar = %q, want %q: no show of a library it cannot open", got, want)
	}
	if got := calendar(admin.ID, domain.CalendarMine); len(got) != 0 {
		t.Errorf("the calendar of a profile's own titles = %q before it began or marked any", got)
	}

	saveShow(tv.ID, "Severance", 1, 2)
	if got := calendar(admin.ID, domain.CalendarAll); slices.Contains(got, "2026-10-17 announced Half Loop") {
		t.Errorf("calendar = %q, still announcing an episode whose file landed", got)
	}
	delete(season, 3)
	describe(severance, season)
	if err := s.MarkWatched(ctx, admin.ID, oneItem(t, s, "kind = 'episode' AND episode_number = 1 AND title = 'Good News About Hell'").ID, nil); err != nil {
		t.Fatal(err)
	}
	want = []string{
		"2026-10-03 available Heat",
		"2026-10-10 available Good News About Hell series_premiere watched",
		"2026-10-17 announced Magic Xylophone series_premiere",
		"2026-10-17 available Half Loop season_finale",
	}
	if got := calendar(admin.ID, domain.CalendarAll); !slices.Equal(got, want) {
		t.Errorf("calendar = %q, want %q once the file lands and is described, the season's last listed its finale", got, want)
	}
	if got, want := calendar(admin.ID, domain.CalendarMine), []string{want[1], want[3]}; !slices.Equal(got, want) {
		t.Errorf("the calendar of a profile's own titles = %q, want %q: the show it began", got, want)
	}
	if err := s.Watchlist(ctx, admin.ID, oneItem(t, s, "title = 'Heat'").ID); err != nil {
		t.Fatal(err)
	}
	if got := calendar(admin.ID, domain.CalendarWatchlist); !slices.Equal(got, []string{"2026-10-03 available Heat"}) {
		t.Errorf("the calendar of the watchlist = %q, want Heat alone", got)
	}
	kids, err := s.Calendar(ctx, CalendarQuery{Profile: admin.ID, Start: day(1), End: day(31), Filter: domain.CalendarAll, Library: kidsTV.ID})
	if err != nil || len(kids) != 1 || len(kids[0].Entries) != 1 || kids[0].Entries[0].Title != "Magic Xylophone" {
		t.Errorf("one library's calendar = %+v, %v; want Bluey's episode alone", kids, err)
	}
	days, err := s.Calendar(ctx, CalendarQuery{Profile: admin.ID, Start: day(11), End: day(16), Filter: domain.CalendarAll})
	if err != nil || len(days) != 0 {
		t.Errorf("a week with nothing out = %v, %v, want no days", days, err)
	}
}
