package animelist

import (
	"os"
	"slices"
	"testing"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// read reads the lists' entries for One Piece, Attack on Titan, Crest of the Stars and Princess
// Mononoke, as they are published.
func read(t *testing.T) []Entry {
	t.Helper()
	places, err := os.Open("testdata/anime-list.xml")
	if err != nil {
		t.Fatal(err)
	}
	defer places.Close()
	ids, err := os.Open("testdata/anime-list-full.json")
	if err != nil {
		t.Fatal(err)
	}
	defer ids.Close()
	entries, err := Read(places, ids)
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

func of(entries []Entry, p domain.Provider, show int) []Entry {
	var out []Entry
	for _, e := range entries {
		if e.Placing(p).Show == show {
			out = append(out, e)
		}
	}
	return out
}

func TestAnEpisodeIsFoundInTheEntryItFallsIn(t *testing.T) {
	entries := read(t)
	titan, piece, stars := of(entries, domain.ProviderTVDB, 267440), of(entries, domain.ProviderTMDB, 37854), of(entries, domain.ProviderTVDB, 72025)
	for _, tc := range []struct {
		name            string
		entries         []Entry
		p               domain.Provider
		season, episode int
		anidb, at       int
		special         bool
	}{
		{"a season's first cour", titan, domain.ProviderTVDB, 3, 12, 13241, 12, false},
		{"its second cour", titan, domain.ProviderTVDB, 3, 13, 14444, 1, false},
		{"the third cour of a season", titan, domain.ProviderTVDB, 4, 29, 17303, 1, false},
		{"a TMDB season mapped by its range", piece, domain.ProviderTMDB, 2, 1, 69, 62, false},
		{"the absolute order", of(entries, domain.ProviderTVDB, 81797), domain.ProviderTVDB, Absolute, 892, 69, 892, false},
		{"TMDB's absolute order, past its seasons' ranges", piece, domain.ProviderTMDB, Absolute, 892, 69, 892, false},
		{"a special listed one by one", of(entries, domain.ProviderTVDB, 81797), domain.ProviderTVDB, 0, 27, 69, 1, true},
		{"an OVA among the specials", stars, domain.ProviderTVDB, 0, 3, 884, 1, false},
	} {
		got, ok := Episode(tc.entries, tc.p, tc.season, tc.episode)
		if !ok || got.Entry.AniDB != tc.anidb || got.Episode != tc.at || got.Special != tc.special {
			t.Errorf("%s: %+v, %v; want AniDB %d episode %d", tc.name, got, ok, tc.anidb, tc.at)
		}
	}
	if got, ok := Episode(titan, domain.ProviderTVDB, 5, 1); ok {
		t.Errorf("a season no entry is in: %+v", got)
	}
}

func TestASeasonAndAShowBeginWithTheirFirstEntry(t *testing.T) {
	entries := read(t)
	titan := of(entries, domain.ProviderTVDB, 267440)
	if e, ok := First(titan, domain.ProviderTVDB, 3); !ok || e.AniDB != 13241 || e.AniList != 99147 {
		t.Errorf("season 3 begins with %+v, %v; want AniDB 13241, AniList 99147", e, ok)
	}
	if e, ok := Opening(titan, domain.ProviderTVDB); !ok || e.AniDB != 9541 {
		t.Errorf("the show begins with %+v, %v; want AniDB 9541", e, ok)
	}
	if e, ok := Opening(of(entries, domain.ProviderTVDB, 81797), domain.ProviderTVDB); !ok || e.AniList != 21 {
		t.Errorf("One Piece, numbered absolutely, begins with %+v, %v; want AniList 21", e, ok)
	}
	if e, ok := First(of(entries, domain.ProviderTMDB, 37854), domain.ProviderTMDB, 2); !ok || e.AniDB != 69 {
		t.Errorf("One Piece's second TMDB season, mapped by a range, begins with %+v, %v; want AniDB 69", e, ok)
	}
}

func TestAFilmIsKnownByItsTMDBAndIMDbIDs(t *testing.T) {
	for _, e := range read(t) {
		if e.AniDB == 7 {
			if e.TVDB.Show != 0 || len(e.TMDBMovies) != 1 || e.TMDBMovies[0] != 128 || e.IMDb[0] != "tt0119698" || e.AniList != 164 {
				t.Errorf("Princess Mononoke = %+v", e)
			}
			return
		}
	}
	t.Error("no Princess Mononoke")
}

func TestAnEntrysEpisodeIsPlacedInTheShow(t *testing.T) {
	entries := read(t)
	byID := map[int]Entry{}
	for _, e := range entries {
		byID[e.AniDB] = e
	}
	for _, tc := range []struct {
		name    string
		anidb   int
		p       domain.Provider
		order   domain.EpisodeOrder
		special bool
		ep      int
		season  int
		at      []int
	}{
		{"a second cour's first", 14444, domain.ProviderTVDB, domain.OrderAired, false, 1, 3, []int{13}},
		{"a special listed one by one", 69, domain.ProviderTVDB, domain.OrderAired, true, 1, 0, []int{27}},
		{"another special listed", 69, domain.ProviderTVDB, domain.OrderAired, true, 16, 0, []int{31}},
		{"by a TMDB range", 69, domain.ProviderTMDB, domain.OrderAired, false, 62, 2, []int{1}},
		{"by a TheTVDB range, as aired", 69, domain.ProviderTVDB, domain.OrderAired, false, 892, 21, []int{1}},
		{"in the absolute order", 69, domain.ProviderTVDB, domain.OrderAbsolute, false, 892, Absolute, []int{892}},
		{"a special, in the absolute order", 69, domain.ProviderTVDB, domain.OrderAbsolute, true, 1, 0, []int{27}},
	} {
		season, at, ok := byID[tc.anidb].At(tc.p, tc.order, tc.special, tc.ep)
		if !ok || season != tc.season || !slices.Equal(at, tc.at) {
			t.Errorf("%s: season %d %v, %v; want season %d %v", tc.name, season, at, ok, tc.season, tc.at)
		}
	}
	if season, at, ok := byID[69].At(domain.ProviderTVDB, domain.OrderAired, true, 6); ok {
		t.Errorf("a special the show has not: season %d %v", season, at)
	}
	var in []int
	for _, e := range In(of(entries, domain.ProviderTVDB, 267440), domain.ProviderTVDB, 3) {
		in = append(in, e.AniDB)
	}
	if !slices.Equal(in, []int{13241, 14444}) {
		t.Errorf("Attack on Titan's season 3 holds %v; want both its cours", in)
	}
}
