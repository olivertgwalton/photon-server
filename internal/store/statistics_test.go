//go:build integration

package store

import (
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// A report counts what was watched over its span: altogether, by each profile, by the titles watched
// most, a show standing for its episodes, and day by day in the zone it is read in, the days nothing
// was watched among them.
func TestStatisticsAddUpWhatWasWatched(t *testing.T) {
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
	if _, err := s.SaveFolder(ctx, films.ID, "Heat", []byte("v1"), []Film{{Title: "Heat", Folder: "Heat"}}, nil); err != nil {
		t.Fatal(err)
	}
	var episodes []Episode
	for n := range 2 {
		episodes = append(episodes, Episode{
			Season: 1, Episodes: []int{n + 1}, Title: "Cosmos", Folder: "Cosmos/Season 1", ByNumber: true,
		})
	}
	if _, err := s.SaveShowFolder(ctx, tv.ID, "Cosmos/Season 1", []byte("v1"), Show{Title: "Cosmos", Folder: "Cosmos"}, episodes, nil); err != nil {
		t.Fatal(err)
	}
	heat, show := oneItem(t, s, `kind = 'movie'`).ID, oneItem(t, s, `kind = 'show'`).ID
	first, second := oneItem(t, s, `kind = 'episode' AND episode_number = 1`).ID, oneItem(t, s, `kind = 'episode' AND episode_number = 2`).ID
	oliver, err := s.AddProfile(ctx, "Oliver", domain.RoleAdmin, "hash", nil)
	if err != nil {
		t.Fatal(err)
	}
	kid, err := s.AddProfile(ctx, "Kid", domain.RoleUser, "hash", nil)
	if err != nil {
		t.Fatal(err)
	}

	// New York, where 11pm on the 9th is 4am on the 10th in UTC.
	zone, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().In(zone)
	day := func(back int, hour int) time.Time {
		d := now.AddDate(0, 0, -back)
		return time.Date(d.Year(), d.Month(), d.Day(), hour, 0, 0, 0, zone)
	}
	play := func(who, what uuid.UUID, stopped time.Time, ran time.Duration) {
		t.Helper()
		p := domain.Playback{Profile: who, Item: what, Method: domain.PlayDirect, Started: stopped.Add(-ran)}
		if err := s.RecordPlay(ctx, p, stopped, ran); err != nil {
			t.Fatal(err)
		}
	}
	play(oliver.ID, heat, day(2, 23), 2*time.Hour)  // late in the evening, two days ago
	play(oliver.ID, first, day(0, 1), time.Hour)    // early today
	play(kid.ID, second, day(0, 2), time.Hour)      // early today
	play(kid.ID, first, day(0, 3), 30*time.Minute)  // early today, the first again
	play(oliver.ID, heat, day(40, 20), 2*time.Hour) // before the span

	since := day(2, 0)
	stats, err := s.Statistics(ctx, since, zone, uuid.UUID{})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Plays != 4 || stats.WatchedMS != (4*time.Hour+30*time.Minute).Milliseconds() {
		t.Errorf("altogether %+v, want four plays over four and a half hours", stats.Watched)
	}

	// Each day of the span is there, today's three plays on today, and Heat on the day it was
	// watched where it was watched, not the next day in UTC.
	if len(stats.Days) != 3 {
		t.Fatalf("days = %+v, want the three of the span", stats.Days)
	}
	for n, want := range []int{1, 0, 3} {
		if got := stats.Days[n].Plays; got != want {
			t.Errorf("day %d (%s): %d plays, want %d", n, time.Time(stats.Days[n].Day).Format(time.DateOnly), got, want)
		}
	}
	if got := time.Time(stats.Days[0].Day).Format(time.DateOnly); got != since.Format(time.DateOnly) {
		t.Errorf("the span starts on %s, want %s", got, since.Format(time.DateOnly))
	}

	// Oliver watched the longest; the kid watched one and a half hours.
	if len(stats.Profiles) != 2 || stats.Profiles[0].Name != "Oliver" || stats.Profiles[1].Profile != kid.ID ||
		stats.Profiles[1].WatchedMS != (90*time.Minute).Milliseconds() {
		t.Errorf("profiles = %+v, want Oliver ahead of the kid's ninety minutes", stats.Profiles)
	}

	// Cosmos's three episode plays count for the show, ahead of Heat's one in the span.
	if len(stats.Titles) != 2 || stats.Titles[0].Card.ID != show || stats.Titles[0].Plays != 3 ||
		stats.Titles[1].Card.ID != heat || stats.Titles[1].Plays != 1 {
		t.Errorf("titles = %+v, want Cosmos's three plays, then Heat's one", stats.Titles)
	}

	// One profile's report is that profile's alone.
	theirs, err := s.Statistics(ctx, since, zone, kid.ID)
	if err != nil {
		t.Fatal(err)
	}
	if theirs.Plays != 2 || len(theirs.Profiles) != 1 || len(theirs.Titles) != 1 || theirs.Titles[0].Card.ID != show {
		t.Errorf("the kid's = %+v, want its two plays, of Cosmos", theirs)
	}

	// A span nothing was watched in still has its days.
	quiet, err := s.Statistics(ctx, day(0, 0), zone, uuid.NewV7())
	if err != nil || quiet.Plays != 0 || len(quiet.Days) != 1 || len(quiet.Titles) != 0 {
		t.Errorf("a profile that watched nothing: %+v, %v", quiet, err)
	}
}
