package naming

import (
	"testing"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

func TestArtwork(t *testing.T) {
	for _, tc := range []struct {
		name string
		stem string
		kind domain.ArtworkKind
		ok   bool
	}{
		{"poster.jpg", "", domain.ArtworkPoster, true},
		{"Folder.JPG", "", domain.ArtworkPoster, true},
		{"fanart.png", "", domain.ArtworkBackdrop, true},
		{"backdrop2.jpg", "", domain.ArtworkBackdrop, true},
		{"fanart-3.jpg", "", domain.ArtworkBackdrop, true},
		{"clearlogo.png", "", domain.ArtworkLogo, true},
		{"landscape.jpg", "", domain.ArtworkThumb, true},
		{"Heat (1995)-poster.jpg", "Heat (1995)", domain.ArtworkPoster, true},
		{"Heat (1995) - 2160p-fanart.jpg", "Heat (1995) - 2160p", domain.ArtworkBackdrop, true},
		{"The Wire S01E01-thumb.jpg", "The Wire S01E01", domain.ArtworkThumb, true},
		{"Spider-Man (2002).jpg", "Spider-Man (2002)", "", true},
		{"logo2.png", "logo2", "", true},
		{"poster.nfo", "", "", false},
	} {
		stem, kind, ok := Artwork(tc.name)
		if stem != tc.stem || kind != tc.kind || ok != tc.ok {
			t.Errorf("Artwork(%q) = %q, %q, %v; want %q, %q, %v", tc.name, stem, kind, ok, tc.stem, tc.kind, tc.ok)
		}
	}
}

func TestSeasonArtwork(t *testing.T) {
	for _, tc := range []struct {
		name   string
		season int
		kind   domain.ArtworkKind
		ok     bool
	}{
		{"season01-poster.jpg", 1, domain.ArtworkPoster, true},
		{"Season2-fanart.png", 2, domain.ArtworkBackdrop, true},
		{"season-specials-poster.jpg", 0, domain.ArtworkPoster, true},
		{"season01-episode.jpg", 0, "", false},
		{"season01.jpg", 0, "", false},
	} {
		season, kind, ok := SeasonArtwork(tc.name)
		if season != tc.season || kind != tc.kind || ok != tc.ok {
			t.Errorf("SeasonArtwork(%q) = %d, %q, %v; want %d, %q, %v", tc.name, season, kind, ok, tc.season, tc.kind, tc.ok)
		}
	}
}
