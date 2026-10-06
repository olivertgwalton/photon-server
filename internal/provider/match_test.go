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
		{"The Thing", 1990, ""},
		{"Thing", 1982, ""},
	} {
		if got := pick(found, tc.title, tc.year); got != tc.want {
			t.Errorf("pick(%q, %d) = %q, want %q", tc.title, tc.year, got, tc.want)
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
