//go:build integration

package store

import (
	"testing"
	"time"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// A remote show has no files to number its episodes by: its first match asks for every season, and
// the episodes its provider says have aired are its episodes. One yet to air is only announced,
// and later matches ask, as a folder show's do, for what is airing.
func TestARemoteShowsEpisodesAreThoseAired(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddRemoteLibrary(ctx, "Popular", domain.LibraryShows, domain.PluginSource("aio"), "series/top", domain.PluginSource("aio"))
	if err != nil {
		t.Fatal(err)
	}
	listed := domain.Listed{Kind: domain.ItemShow, IDs: map[domain.Provider]string{domain.ProviderIMDb: "tt0306414"}, Title: "The Wire"}
	if _, err := s.SaveListed(ctx, lib.ID, domain.ItemShow, []domain.Listed{listed}); err != nil {
		t.Fatal(err)
	}
	show := oneItem(t, s, "kind = 'show'")
	sub, ok, err := s.IdentifySubject(ctx, show.ID)
	if err != nil || !ok || sub.Scope != domain.SeasonsEvery {
		t.Fatalf("subject %+v, %v; want every season asked for", sub, err)
	}

	aired, coming := time.Now().AddDate(0, 0, -7), time.Now().AddDate(0, 0, 7)
	err = s.SaveIdentity(ctx, show.ID, domain.SourceTMDB, domain.Metadata{Title: "The Wire"}, map[int]domain.SeasonMetadata{
		1: {Episodes: map[int]domain.Metadata{
			1: {Title: "The Target", ReleaseDate: aired},
			2: {Title: "The Detail", ReleaseDate: aired},
		}},
		2: {Episodes: map[int]domain.Metadata{1: {Title: "Ebb Tide", ReleaseDate: coming}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, s, `SELECT count(*) FROM items WHERE kind = 'season'`); n != 1 {
		t.Errorf("%d seasons, want season 1 alone: season 2 has nothing aired", n)
	}
	if got := oneItem(t, s, "kind = 'episode' AND episode_number = 1"); got.Title != "The Target" || got.AirDate == nil {
		t.Errorf("the first episode is %+v, want The Target with the day it aired", got)
	}
	if n := countRows(t, s, `SELECT count(*) FROM items WHERE kind = 'episode'`); n != 2 {
		t.Errorf("%d episodes, want the two aired", n)
	}
	if n := countRows(t, s, `SELECT count(*) FROM announced_episodes WHERE season_number = 2`); n != 1 {
		t.Errorf("%d of season 2 announced, want Ebb Tide", n)
	}

	if sub, _, err := s.IdentifySubject(ctx, show.ID); err != nil || sub.Scope != domain.SeasonsNumbered {
		t.Errorf("matched again: %+v, %v; want what is airing asked for", sub, err)
	}
	// Matched again, the same episodes are not added twice.
	err = s.SaveIdentity(ctx, show.ID, domain.SourceTMDB, domain.Metadata{Title: "The Wire"}, map[int]domain.SeasonMetadata{
		1: {Episodes: map[int]domain.Metadata{1: {Title: "The Target", ReleaseDate: aired}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, s, `SELECT count(*) FROM items WHERE kind = 'episode'`); n != 2 {
		t.Errorf("matched again, %d episodes, want the same two", n)
	}
}
