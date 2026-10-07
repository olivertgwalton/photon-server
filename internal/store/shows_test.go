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

// A show's episodes are read all at once, every season's in order, or one season's; and not by a
// profile that may not see the show.
func TestAShowsEpisodesInOrder(t *testing.T) {
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
	for _, se := range [][2]int{{2, 1}, {1, 2}, {1, 1}} {
		rel := fmt.Sprintf("Wire/S%dE%d.mkv", se[0], se[1])
		eps = append(eps, Episode{
			Season: se[0], Episodes: []int{se[1]}, Title: fmt.Sprintf("S%dE%d", se[0], se[1]), Folder: "Wire", ByNumber: true,
			Copies: []Copy{{ContentKey: []byte(rel), Parts: []Part{{RelPath: rel, Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{Duration: time.Hour}}}}},
		})
	}
	if _, err := s.SaveShowFolder(ctx, tv.ID, "Wire", []byte("v"), Show{Title: "The Wire", Folder: "Wire"}, eps, nil); err != nil {
		t.Fatal(err)
	}
	show := oneItem(t, s, `kind = 'show'`).ID
	titles := func(cards []Card) []string {
		var out []string
		for _, c := range cards {
			out = append(out, c.Title)
		}
		return out
	}
	all, err := s.Episodes(ctx, profile.ID, show)
	if err != nil || !slices.Equal(titles(all), []string{"S1E1", "S1E2", "S2E1"}) {
		t.Errorf("the show's episodes: %v, %v", titles(all), err)
	}
	seasons, err := s.Seasons(ctx, profile.ID, show)
	if err != nil || len(seasons) != 2 || seasons[0].Episodes != 2 {
		t.Fatalf("seasons: %+v, %v", seasons, err)
	}
	one, err := s.Episodes(ctx, profile.ID, seasons[1].ID)
	if err != nil || !slices.Equal(titles(one), []string{"S2E1"}) || one[0].Show == nil || one[0].Season == nil {
		t.Errorf("season 2's: %v, %v", titles(one), err)
	}

	kid, err := s.AddProfile(ctx, "Kid", domain.RoleRestricted, "hash")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetAccess(ctx, kid.ID, ProfileAccess{Libraries: []uuid.UUID{films.ID}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Episodes(ctx, kid.ID, show); !errors.Is(err, ErrNotFound) {
		t.Errorf("a show the profile may not see: %v, want ErrNotFound", err)
	}
	if _, err := s.Seasons(ctx, kid.ID, show); !errors.Is(err, ErrNotFound) {
		t.Errorf("its seasons: %v, want ErrNotFound", err)
	}
}
