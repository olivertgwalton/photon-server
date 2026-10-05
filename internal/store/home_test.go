//go:build integration

package store

import (
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/media"
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
			RelPath: rel, Size: 1, ModTime: time.Unix(0, 0), Facts: &media.Facts{Duration: time.Hour},
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
		i := s.q.Item
		row, err := i.WithContext(ctx).Where(i.Kind.Eq(string(domain.ItemEpisode)), i.SeasonNumber.Eq(season), i.EpisodeNumber.Eq(n)).Take()
		if err != nil {
			t.Fatal(err)
		}
		return uuid.UUID(row.ID)
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

	if err := s.MarkWatched(ctx, profile.ID, episode(1, 1)); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkWatched(ctx, profile.ID, episode(0, 1)); err != nil {
		t.Fatal(err)
	}
	if got := home()[domain.RowNextUp]; len(got) != 1 || got[0] != "Wire/S1E2.mkv" {
		t.Errorf("next up = %v, want the episode after the last one watched, specials aside", got)
	}

	if _, err := s.SaveProgress(ctx, profile.ID, episode(1, 2), 20*time.Minute); err != nil {
		t.Fatal(err)
	}
	heat, err := s.q.Item.WithContext(ctx).Where(s.q.Item.Kind.Eq(string(domain.ItemMovie))).Take()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveProgress(ctx, profile.ID, uuid.UUID(heat.ID), 30*time.Minute); err != nil {
		t.Fatal(err)
	}
	got := home()
	if c := got[domain.RowContinueWatching]; len(c) != 2 || c[0] != "Heat" {
		t.Errorf("continue watching = %v, want Heat, played last, then the episode", c)
	}
	if len(got[domain.RowNextUp]) != 0 {
		t.Errorf("next up = %v, want nothing: the next episode is under way", got[domain.RowNextUp])
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
}
