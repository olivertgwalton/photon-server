//go:build integration

package store

import (
	"slices"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// A collection with no poster of its own wears a mosaic of the first four of its titles' posters
// each viewer may see, as Plex draws one; one title lends its own poster.
func TestACollectionWithNoPosterWearsAMosaicOfItsTitles(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films", "")
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]uuid.UUID{}
	posters := map[string]uuid.UUID{}
	for _, f := range []struct{ title, cert string }{{"Alien", "15"}, {"Brazil", "15"}, {"Casino", "18"}, {"Dune", "12"}, {"Emma", "U"}} {
		film := Film{
			Title: f.title, Folder: f.title, Artwork: []domain.Artwork{{Kind: domain.ArtworkPoster, Path: f.title + "/poster.jpg"}},
			Copies: []Copy{{ContentKey: []byte(f.title), Parts: []Part{{
				RelPath: f.title + "/" + f.title + ".mkv", Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{},
			}}}},
		}
		if _, err := s.SaveFolder(ctx, lib.ID, f.title, []byte("v1"), []Film{film}, nil); err != nil {
			t.Fatal(err)
		}
		var id uuid.UUID
		if err := s.pool.QueryRow(ctx, `SELECT id FROM items WHERE title = $1`, f.title).Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids[f.title] = id
		if err := s.SaveIdentity(ctx, id, domain.SourceTMDB, domain.Metadata{Title: f.title, Certificate: f.cert}, nil); err != nil {
			t.Fatal(err)
		}
		page, err := s.Title(ctx, uuid.UUID{}, id)
		if err != nil || len(page.Artwork[domain.ArtworkPoster]) != 1 {
			t.Fatal(page.Artwork, err)
		}
		posters[f.title] = page.Artwork[domain.ArtworkPoster][0]
	}
	set, err := s.AddCollection(ctx, "Five")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetMembers(ctx, set, []uuid.UUID{ids["Alien"], ids["Brazil"], ids["Casino"], ids["Dune"], ids["Emma"]}); err != nil {
		t.Fatal(err)
	}
	teen, err := s.AddProfile(ctx, "Teen", domain.RoleUser, "hash", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetAccess(ctx, teen.ID, ProfileAccess{MaxAge: new(15), Unrated: domain.UnratedAllow}, nil); err != nil {
		t.Fatal(err)
	}
	poster := func(profile uuid.UUID) uuid.UUID {
		t.Helper()
		cards, _, err := totalled(s.Collections(ctx, lib.ID, profile, totalledPage(0, 10)))
		if err != nil || len(cards) != 1 {
			t.Fatal(cards, err)
		}
		page, err := s.Title(ctx, profile, set)
		if err != nil || len(page.Artwork[domain.ArtworkPoster]) != 1 || page.Artwork[domain.ArtworkPoster][0] != cards[0].Poster {
			t.Fatalf("its page wears %v, %v; want its card's %v", page.Artwork, err, cards[0].Poster)
		}
		return cards[0].Poster
	}
	tiles := func(mosaic uuid.UUID) []uuid.UUID {
		t.Helper()
		p, err := s.Picture(ctx, mosaic)
		if err != nil {
			t.Fatal(err)
		}
		var out []uuid.UUID
		for _, tile := range p.Mosaic {
			if tile.Picture.Path == "" {
				t.Errorf("tile %v is nowhere: %+v", tile.ID, tile.Picture)
			}
			out = append(out, tile.ID)
		}
		return out
	}

	everyone := poster(uuid.UUID{})
	if got, want := tiles(everyone), []uuid.UUID{posters["Alien"], posters["Brazil"], posters["Casino"], posters["Dune"]}; !slices.Equal(got, want) {
		t.Errorf("mosaic = %v, want the first four titles' posters in order %v", got, want)
	}
	// A profile kept to 15 is shown no poster of a film it may not see.
	kept := poster(teen.ID)
	if got, want := tiles(kept), []uuid.UUID{posters["Alien"], posters["Brazil"], posters["Dune"], posters["Emma"]}; !slices.Equal(got, want) {
		t.Errorf("a profile kept to 15's mosaic = %v, want %v", got, want)
	}
	// What the collection holds changing forgets its mosaics, which are then swept away.
	if err := s.SetMembers(ctx, set, []uuid.UUID{ids["Emma"]}); err != nil {
		t.Fatal(err)
	}
	if live, err := s.LivePictures(ctx, []uuid.UUID{everyone, kept}); err != nil || live[everyone] || live[kept] {
		t.Errorf("mosaics live after the collection changed: %v, %v", live, err)
	}
	if got := poster(uuid.UUID{}); got != posters["Emma"] {
		t.Errorf("a collection of one wears %v, want its title's poster %v", got, posters["Emma"])
	}
}
