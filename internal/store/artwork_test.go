//go:build integration

package store

import (
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/media"
)

func TestPicturesBesideATitleComeBeforeAProvidersAndItsCardShowsTheBest(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	film := Film{
		Title: "heat", Folder: "Heat", Artwork: []domain.Artwork{{Kind: domain.ArtworkPoster, Path: "Heat/poster.jpg"}},
		Copies: []Copy{{ContentKey: []byte("heat"), Parts: []Part{{
			RelPath: "Heat/Heat.mkv", Size: 1, ModTime: time.Unix(0, 0), Facts: &media.Facts{},
		}}}},
	}
	if _, err := s.SaveFolder(ctx, lib.ID, "Heat", []byte("v1"), []Film{film}, nil); err != nil {
		t.Fatal(err)
	}
	item, err := s.q.Item.WithContext(ctx).Take()
	if err != nil {
		t.Fatal(err)
	}
	err = s.SaveIdentity(ctx, uuid.UUID(item.ID), domain.SourceTMDB, domain.Metadata{Artwork: []domain.Artwork{
		{Kind: domain.ArtworkPoster, URL: "https://image.tmdb.org/t/p/original/heat.jpg"},
		{Kind: domain.ArtworkBackdrop, URL: "https://image.tmdb.org/t/p/original/heat-wide.jpg"},
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	page, err := s.Title(ctx, uuid.UUID{}, uuid.UUID(item.ID))
	if err != nil {
		t.Fatal(err)
	}
	posters := page.Artwork[domain.ArtworkPoster]
	if len(posters) != 2 || len(page.Artwork[domain.ArtworkBackdrop]) != 1 {
		t.Fatalf("artwork = %v, want two posters and a backdrop", page.Artwork)
	}
	local, err := s.Picture(ctx, posters[0])
	if err != nil || local.Path != "Heat/poster.jpg" || local.Root != "/srv/films" {
		t.Errorf("best poster = %+v, %v; want the file beside the film", local, err)
	}
	if provider, _ := s.Picture(ctx, posters[1]); provider.URL != "https://image.tmdb.org/t/p/original/heat.jpg" {
		t.Errorf("second poster = %+v, want TMDB's", provider)
	}
	cards, _, err := s.Wall(ctx, lib.ID, WallPage{Sort: domain.SortTitle, Order: domain.Ascending, Limit: 10})
	if err != nil || len(cards) != 1 || cards[0].Poster != posters[0] || cards[0].Backdrop != page.Artwork[domain.ArtworkBackdrop][0] {
		t.Errorf("card = %+v, %v; want the best poster and backdrop", cards, err)
	}
}
