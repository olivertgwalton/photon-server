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

// A special its source places is watched in the season it aired in: on that season's page, up
// next, next up and in the show's play order. The specials' own page keeps them all in their own
// order, and one placed nowhere stays among them alone.
func TestASpecialIsWatchedInTheSeasonItAiredIn(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	tv, err := s.AddLibrary(ctx, "TV", domain.LibraryShows, "/srv/tv", "")
	if err != nil {
		t.Fatal(err)
	}
	profile, err := s.AddProfile(ctx, "Oliver", domain.RoleAdmin, "hash", nil)
	if err != nil {
		t.Fatal(err)
	}
	var eps []Episode
	for _, se := range [][2]int{{1, 1}, {1, 2}, {1, 3}, {2, 1}, {2, 2}, {0, 1}, {0, 2}, {0, 3}, {0, 4}} {
		rel := fmt.Sprintf("Wire/S%dE%d.mkv", se[0], se[1])
		eps = append(eps, Episode{
			Season: se[0], Episodes: []int{se[1]}, Title: fmt.Sprintf("S%dE%d", se[0], se[1]), Folder: "Wire", ByNumber: true,
			Versions: []Version{{ContentKey: []byte(rel), Parts: []Part{{RelPath: rel, Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{Duration: time.Hour}}}}},
		})
	}
	if _, err := s.SaveShowFolder(ctx, tv.ID, "Wire", []byte("v"), Show{Title: "The Wire", Folder: "Wire"}, eps, nil); err != nil {
		t.Fatal(err)
	}
	show := oneItem(t, s, `kind = 'show'`).ID
	// S0E1 aired before S2E2, S0E2 after season 1, S0E3 at season 2's start; S0E4 is placed nowhere.
	if err := s.SaveIdentity(ctx, show, domain.SourceTMDB, domain.Metadata{}, map[int]domain.SeasonMetadata{0: {Episodes: map[int]domain.Metadata{
		1: {AirsBeforeSeason: 2, AirsBeforeEpisode: 2}, 2: {AirsAfterSeason: 1}, 3: {AirsBeforeSeason: 2},
	}}}); err != nil {
		t.Fatal(err)
	}
	episode := func(season, n int) uuid.UUID {
		t.Helper()
		return oneItem(t, s, `kind = 'episode' AND season_number = $1 AND episode_number = $2`, season, n).ID
	}
	season := func(n int) uuid.UUID {
		t.Helper()
		return oneItem(t, s, `kind = 'season' AND season_number = $1`, n).ID
	}
	page := func(n int) []string {
		t.Helper()
		p, err := s.Title(ctx, profile.ID, season(n))
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, e := range p.Episodes {
			out = append(out, fmt.Sprintf("S%dE%d", *e.Season, *e.Number))
		}
		return out
	}
	for n, want := range map[int][]string{
		1: {"S1E1", "S1E2", "S1E3", "S0E2"},
		2: {"S0E3", "S2E1", "S0E1", "S2E2"},
		0: {"S0E1", "S0E2", "S0E3", "S0E4"},
	} {
		if got := page(n); !slices.Equal(got, want) {
			t.Errorf("season %d lists %v, want %v", n, got, want)
		}
	}
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
		after uuid.UUID
		want  string
	}{
		{episode(1, 3), "S0E2"}, {episode(0, 2), "S0E3"}, {episode(2, 1), "S0E1"}, {episode(2, 2), "none"},
	} {
		if got := next(tc.after); got != tc.want {
			t.Errorf("after %v: %s, want %s", tc.after, got, tc.want)
		}
	}
	if err := s.MarkWatched(ctx, profile.ID, episode(2, 1), nil); err != nil {
		t.Fatal(err)
	}
	if got := everyRow(ctx, t, s, profile.ID)[domain.RowNextUp]; len(got) != 1 || got[0].Title != "S0E1" {
		t.Errorf("next up = %+v, want the special that aired before S2E2", got)
	}
	plays, err := playableOf(ctx, s.pool, show)
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, id := range plays {
		order = append(order, oneItem(t, s, `id = $1`, id).Title)
	}
	if want := []string{"S1E1", "S1E2", "S1E3", "S0E2", "S0E3", "S2E1", "S0E1", "S2E2", "S0E4"}; !slices.Equal(order, want) {
		t.Errorf("the show plays %v, want %v", order, want)
	}
}
