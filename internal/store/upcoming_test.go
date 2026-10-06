//go:build integration

package store

import (
	"slices"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

func TestUpcomingListsTheShowsDueToAirSoonestFirst(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	tv, err := s.AddLibrary(ctx, "TV", domain.LibraryShows, "/srv/tv")
	if err != nil {
		t.Fatal(err)
	}
	kids, err := s.AddLibrary(ctx, "Kids", domain.LibraryShows, "/srv/kids")
	if err != nil {
		t.Fatal(err)
	}
	var today time.Time
	if err := s.pool.QueryRow(ctx, `SELECT current_date`).Scan(&today); err != nil {
		t.Fatal(err)
	}
	show := func(lib uuid.UUID, title string, airs *domain.Airing) uuid.UUID {
		t.Helper()
		episode := Episode{
			Season: 1, Episodes: []int{1}, Title: title, Folder: title + "/Season 1", ByNumber: true,
			Copies: []Copy{{ContentKey: []byte(title), Parts: []Part{{
				RelPath: title + "/Season 1/1.mkv", Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{},
			}}}},
		}
		if _, err := s.SaveShowFolder(ctx, lib, title+"/Season 1", []byte("v1"), Show{Title: title, Folder: title}, []Episode{episode}, nil); err != nil {
			t.Fatal(err)
		}
		var id uuid.UUID
		if err := s.pool.QueryRow(ctx, `SELECT id FROM items WHERE kind = 'show' AND title = $1`, title).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if err := s.SaveIdentity(ctx, id, domain.SourceTMDB, domain.Metadata{NextAiring: airs}, nil); err != nil {
			t.Fatal(err)
		}
		return id
	}
	show(tv.ID, "Later", &domain.Airing{SeasonNumber: 2, EpisodeNumber: 5, Title: "Five", Date: today.AddDate(0, 0, 7)})
	show(tv.ID, "Tonight", &domain.Airing{SeasonNumber: 1, EpisodeNumber: 2, Title: "Two", Date: today})
	show(tv.ID, "Aired", &domain.Airing{SeasonNumber: 1, EpisodeNumber: 2, Date: today.AddDate(0, 0, -1)})
	show(tv.ID, "Ended", nil)
	cartoon := show(kids.ID, "Cartoon", &domain.Airing{SeasonNumber: 3, EpisodeNumber: 1, Date: today.AddDate(0, 0, 3)})

	owner, err := s.AddProfile(ctx, "Owner", domain.RoleMember, "")
	if err != nil {
		t.Fatal(err)
	}
	kid, err := s.AddProfile(ctx, "Kid", domain.RoleRestricted, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetAccess(ctx, kid.ID, ProfileAccess{Unrated: domain.UnratedAllow, Libraries: []uuid.UUID{kids.ID}}); err != nil {
		t.Fatal(err)
	}
	upcoming := func(q UpcomingQuery) []string {
		t.Helper()
		q.Limit = max(q.Limit, 10)
		got, total, err := s.Upcoming(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, u := range got {
			out = append(out, u.Show.Title)
		}
		if q.Offset == 0 && int(total) != len(out) {
			t.Errorf("total = %d for %q", total, out)
		}
		return out
	}

	if got := upcoming(UpcomingQuery{Profile: owner.ID}); !slices.Equal(got, []string{"Tonight", "Cartoon", "Later"}) {
		t.Errorf("upcoming = %q, want those airing today or later, soonest first", got)
	}
	if got := upcoming(UpcomingQuery{Profile: owner.ID, Library: tv.ID}); !slices.Equal(got, []string{"Tonight", "Later"}) {
		t.Errorf("upcoming in TV = %q", got)
	}
	if got := upcoming(UpcomingQuery{Profile: kid.ID}); !slices.Equal(got, []string{"Cartoon"}) {
		t.Errorf("upcoming for a profile that sees Kids alone = %q", got)
	}
	got, _, err := s.Upcoming(ctx, UpcomingQuery{Profile: owner.ID, Limit: 1})
	if err != nil || len(got) != 1 {
		t.Fatal(got, err)
	}
	want := domain.Airing{SeasonNumber: 1, EpisodeNumber: 2, Title: "Two", Date: today}
	if a := got[0].Airing; a.SeasonNumber != want.SeasonNumber || a.EpisodeNumber != want.EpisodeNumber || a.Title != want.Title || !a.Date.Equal(want.Date) {
		t.Errorf("next airing = %+v, want %+v", a, want)
	}

	// TMDB saying the show has nothing more to air takes it off the list.
	if err := s.SaveIdentity(ctx, cartoon, domain.SourceTMDB, domain.Metadata{}, nil); err != nil {
		t.Fatal(err)
	}
	if got := upcoming(UpcomingQuery{Profile: kid.ID}); len(got) != 0 {
		t.Errorf("upcoming after the provider knows of no more = %q", got)
	}
}
