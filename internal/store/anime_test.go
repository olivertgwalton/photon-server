//go:build integration

package store

import (
	"testing"

	"github.com/olivertgwalton/photon-server/internal/animelist"
	"github.com/olivertgwalton/photon-server/internal/domain"
)

func TestTheAnimeListsAreKeptAndReplacedWhole(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	titan := []animelist.Entry{
		{AniDB: 13241, AniList: 99147, TVDB: animelist.Placing{Show: 267440, Season: 3}, TMDB: animelist.Placing{Show: 1429, Season: 3}},
		{
			AniDB: 14444, AniList: 104578, TVDB: animelist.Placing{Show: 267440, Season: 3, Offset: 12},
			Mappings: []animelist.Mapping{{Provider: domain.ProviderTVDB, Special: true, Episodes: map[int][]int{1: {9}}}},
		},
		{AniDB: 7, AniList: 164, TMDBMovies: []int{128}, IMDb: []string{"tt0119698"}},
	}
	if err := s.ReplaceAnimeEntries(ctx, titan); err != nil {
		t.Fatal(err)
	}
	got, err := s.AnimeShow(ctx, domain.ProviderTVDB, 267440)
	if err != nil || len(got) != 2 || got[1].Mappings[0].Episodes[1][0] != 9 {
		t.Fatalf("on TheTVDB: %+v, %v; want both cours of season 3, the second's special mapped", got, err)
	}
	if place, ok := animelist.Episode(got, domain.ProviderTVDB, 3, 13); !ok || place.Entry.AniList != 104578 || place.Episode != 1 {
		t.Errorf("season 3 episode 13: %+v, %v; want the second cour's first", place, ok)
	}
	for _, tc := range []struct {
		p     domain.Provider
		id    string
		anidb int
	}{
		{domain.ProviderAniList, "104578", 14444},
		{domain.ProviderAniDB, "13241", 13241},
		{domain.ProviderTMDB, "128", 7},
		{domain.ProviderIMDb, "tt0119698", 7},
	} {
		e, ok, err := s.AnimeEntry(ctx, tc.p, tc.id)
		if err != nil || !ok || e.AniDB != tc.anidb {
			t.Errorf("by %s %s: %+v, %v, %v; want AniDB %d", tc.p, tc.id, e, ok, err, tc.anidb)
		}
	}
	if _, ok, err := s.AnimeEntry(ctx, domain.ProviderAniList, "nope"); ok || err != nil {
		t.Errorf("by an id no number: %v, %v; want none", ok, err)
	}
	if err := s.ReplaceAnimeEntries(ctx, titan[2:]); err != nil {
		t.Fatal(err)
	}
	if got, err := s.AnimeShow(ctx, domain.ProviderTMDB, 1429); err != nil || len(got) != 0 {
		t.Errorf("after a list without it: %+v, %v; want Attack on Titan gone", got, err)
	}
}
