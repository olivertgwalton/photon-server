package provider

import (
	"sync"
	"testing"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

func TestPick(t *testing.T) {
	found := []domain.Candidate{
		{ID: "1", Title: "The Thing", Year: 2011},
		{ID: "2", Title: "The Thing", Year: 1982},
		{ID: "3", Title: "Le Fabuleux Destin d'Amélie Poulain", OriginalTitle: "Amélie", Year: 2001},
		{ID: "4", Title: "Fast & Furious", Year: 2009},
		{ID: "5", Title: "Doctor Who (2005)", Year: 2005},
	}
	for _, tc := range []struct {
		title string
		year  int
		want  string
	}{
		{"The Thing", 1982, "2"},
		{"The Thing", 1983, "2"},
		{"The Thing", 0, "1"},
		{"the thing", 2011, "1"},
		{"Amelie", 2001, "3"},
		{"Fast and Furious", 2009, "4"},
		{"Doctor Who", 2005, "5"},
		// No year near: the provider's first of the name.
		{"The Thing", 1990, "1"},
		// No name alike: the one of the year, over the provider's first.
		{"Thing", 1982, "2"},
	} {
		if got := pick(found, tc.title, tc.year); got != tc.want {
			t.Errorf("pick(%q, %d) = %q, want %q", tc.title, tc.year, got, tc.want)
		}
	}
	if got := pick(nil, "The Thing", 1982); got != "" {
		t.Errorf("no results matched %q", got)
	}
}

// A search finds a title by names its result does not carry: TMDB finds Frieren by its romanised
// Japanese title, and its result is taken, as Jellyfin takes it.
func TestASearchsOwnFirstResultStandsWhereNoneIsNamedAlike(t *testing.T) {
	frieren := []domain.Candidate{{ID: "209867", Title: "Frieren: Beyond Journey's End", OriginalTitle: "葬送のフリーレン", Year: 2023}}
	if got := pick(frieren, "Sousou no Frieren", 2023); got != "209867" {
		t.Errorf("Sousou no Frieren matched %q, want TMDB's one result", got)
	}
	// A name followed by more words outranks the first result, but only by whole words.
	street := []domain.Candidate{{ID: "1", Title: "Wallander"}, {ID: "2", Title: "Wall Street"}}
	if got := pick(street, "Wall", 0); got != "2" {
		t.Errorf("Wall matched %q, want Wall Street over Wallander", got)
	}
}

// TMDB's year only nudges its ranking, so a remake's search puts the original first; a year one off
// is as near as an exact one, so TMDB's order settles two films of a name a year apart. Their
// results as TMDB ranks them, as Jellyfin's own tests of its ranking hold them.
func TestTheYearSettlesAFilmOfOneName(t *testing.T) {
	for _, tc := range []struct {
		title string
		year  int
		found []domain.Candidate
		want  string
	}{
		{"Mulan", 2020, []domain.Candidate{
			{ID: "10674", Title: "Mulan", Year: 1998}, {ID: "337401", Title: "Mulan", Year: 2020}, {ID: "752662", Title: "Hua Mulan", Year: 2020},
		}, "337401"},
		{"The Lion King", 2019, []domain.Candidate{
			{ID: "8587", Title: "The Lion King", Year: 1994}, {ID: "420818", Title: "The Lion King", Year: 2019},
		}, "420818"},
		// Shyamalan's, dated 2017 by TMDB though it premiered in 2016, over an obscure film of 2016.
		{"Split", 2016, []domain.Candidate{
			{ID: "381288", Title: "Split", Year: 2017}, {ID: "358364", Title: "Split", Year: 2016}, {ID: "407223", Title: "Split", Year: 2016},
		}, "381288"},
	} {
		if got := pick(tc.found, tc.title, tc.year); got != tc.want {
			t.Errorf("%s (%d) matched %q, want %q", tc.title, tc.year, got, tc.want)
		}
	}
}

// Identify jobs match titles side by side.
func TestNormaliseIsSafeAtOnce(t *testing.T) {
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 2000 {
				if got := normalise("Amélie (2001)"); got != "amelie" {
					t.Errorf("normalise = %q", got)
					return
				}
			}
		})
	}
	wg.Wait()
}

func TestResolve(t *testing.T) {
	// IMDb knows The Thing (1982); a search finds it only without a year, as one read from a folder
	// may be the wrong release date.
	find := func(p domain.Provider, id string) ([]domain.Candidate, error) {
		if p == domain.ProviderIMDb && id == "tt0084787" {
			return []domain.Candidate{{ID: "1091"}}, nil
		}
		return nil, nil
	}
	search := func(title string, year int) ([]domain.Candidate, error) {
		if title == "The Thing" && year == 0 {
			return []domain.Candidate{{ID: "1091", Title: "The Thing", Year: 1982}}, nil
		}
		return nil, nil
	}
	for _, tc := range []struct {
		name string
		h    Hints
		want string
	}{
		{"its own id", Hints{Title: "The Thing", IDs: map[domain.Provider]string{domain.ProviderTMDB: "42", domain.ProviderIMDb: "tt0084787"}}, "42"},
		{"an own id that is no id", Hints{Title: "The Thing", IDs: map[domain.Provider]string{domain.ProviderTMDB: "thing", domain.ProviderIMDb: "tt0084787"}}, "1091"},
		{"another provider's id", Hints{IDs: map[domain.Provider]string{domain.ProviderIMDb: "tt0084787"}}, "1091"},
		{"another provider's id it does not know", Hints{Title: "The Thing", IDs: map[domain.Provider]string{domain.ProviderIMDb: "tt0000001"}}, "1091"},
		{"a search without the wrong year", Hints{Title: "The Thing", Year: 1985}, "1091"},
		{"nothing found", Hints{Title: "Nothing", Year: 1985}, ""},
	} {
		if got, err := Resolve(tc.h, domain.ProviderTMDB, []domain.Provider{domain.ProviderIMDb, domain.ProviderTVDB}, find, search); err != nil || got != tc.want {
			t.Errorf("%s: %q, %v; want %q", tc.name, got, err, tc.want)
		}
	}
}
