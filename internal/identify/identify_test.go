package identify

import (
	"testing"

	"github.com/olivertgwalton/photon-server/internal/tmdb"
)

func TestPick(t *testing.T) {
	found := []tmdb.Match{
		{ID: 1, Title: "The Thing", Year: 2011},
		{ID: 2, Title: "The Thing", Year: 1982},
		{ID: 3, Title: "Le Fabuleux Destin d'Amélie Poulain", OriginalTitle: "Amélie", Year: 2001},
		{ID: 4, Title: "Fast & Furious", Year: 2009},
	}
	for _, tc := range []struct {
		title string
		year  int
		want  int
	}{
		{"The Thing", 1982, 2},
		{"The Thing", 1983, 2},
		{"The Thing", 0, 1},
		{"the thing", 2011, 1},
		{"Amelie", 2001, 3},
		{"Fast and Furious", 2009, 4},
		{"The Thing", 1990, 0},
		{"Thing", 1982, 0},
	} {
		if got := pick(found, tc.title, tc.year); got != tc.want {
			t.Errorf("pick(%q, %d) = %d, want %d", tc.title, tc.year, got, tc.want)
		}
	}
}
