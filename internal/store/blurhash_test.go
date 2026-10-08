//go:build integration

package store

import (
	"maps"
	"slices"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

func TestPicturesAnswerTheirBlurhashesWhereverTheyAreShown(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	film := Film{
		Title: "Heat", Folder: "Heat",
		Artwork: []domain.Artwork{{Kind: domain.ArtworkPoster, Path: "Heat/poster.jpg", Blurhash: "LEHV6nWB2yk8pyo0adR*.7kCMdnj"}},
		Copies: []Copy{{ContentKey: []byte("heat"), Parts: []Part{{
			RelPath: "Heat/Heat.mkv", Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{},
		}}}},
	}
	if _, err := s.SaveFolder(ctx, lib.ID, "Heat", []byte("v1"), []Film{film}, nil); err != nil {
		t.Fatal(err)
	}
	var item uuid.UUID
	if err := s.pool.QueryRow(ctx, `SELECT id FROM items`).Scan(&item); err != nil {
		t.Fatal(err)
	}
	pacino := func(photo string) domain.Metadata {
		return domain.Metadata{
			Artwork: []domain.Artwork{{Kind: domain.ArtworkBackdrop, URL: "https://image.tmdb.org/t/p/original/heat-wide.jpg"}},
			Credits: []domain.Credit{{Name: "Al Pacino", IDs: map[domain.Provider]string{domain.ProviderTMDB: "1158"}, Photo: photo, Kind: domain.CreditActor}},
		}
	}
	if err := s.SaveIdentity(ctx, item, domain.SourceTMDB, pacino("https://image.tmdb.org/t/p/original/a.jpg"), nil); err != nil {
		t.Fatal(err)
	}

	page, err := s.Title(ctx, uuid.UUID{}, item)
	if err != nil {
		t.Fatal(err)
	}
	poster, backdrop := page.Artwork[domain.ArtworkPoster][0], page.Artwork[domain.ArtworkBackdrop][0]
	photo := page.Credits[0].Photo
	unhashed, err := s.Unhashed(ctx, uuid.UUID{}, 10)
	if err != nil {
		t.Fatal(err)
	}
	want := []Unhashed{{ID: backdrop}, {ID: photo}}
	slices.SortFunc(want, func(a, b Unhashed) int { return a.ID.Compare(b.ID) })
	if !slices.Equal(unhashed, want) {
		t.Errorf("unhashed = %+v, want the provider's backdrop and the photo, to be fetched: %+v", unhashed, want)
	}
	after, err := s.Unhashed(ctx, want[0].ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(after, want[1:]) {
		t.Errorf("unhashed after the first = %+v, want the second alone", after)
	}

	for id, hash := range map[uuid.UUID]string{backdrop: "L6PZfSi_.AyE_3t7t7R**0o#DgR4", photo: "LKO2?U%2Tw=w]~RBVZRi};RPxuwH"} {
		if err := s.SetBlurhash(ctx, id, hash); err != nil {
			t.Fatal(err)
		}
	}
	if page, err = s.Title(ctx, uuid.UUID{}, item); err != nil {
		t.Fatal(err)
	}
	if got := slices.Sorted(maps.Values(page.Blurhashes)); len(got) != 2 || page.Blurhashes[poster] == "" || page.Blurhashes[backdrop] == "" {
		t.Errorf("page blurhashes = %v, want the scanned poster's and the fetched backdrop's", page.Blurhashes)
	}
	if page.Credits[0].Blurhashes[photo] != "LKO2?U%2Tw=w]~RBVZRi};RPxuwH" {
		t.Errorf("credit = %+v, want its photo's blurhash", page.Credits[0])
	}
	cards, _, err := s.Wall(ctx, []uuid.UUID{lib.ID}, WallPage{Sort: domain.SortTitle, Order: domain.Ascending, Limit: 10})
	if err != nil || len(cards) != 1 || len(cards[0].Blurhashes) != 2 {
		t.Errorf("cards = %+v, %v; want the poster's and backdrop's blurhashes", cards, err)
	}
	person, err := s.Person(ctx, page.Credits[0].PersonID)
	if err != nil || person.Blurhashes[photo] == "" {
		t.Errorf("person = %+v, %v; want the photo's blurhash", person, err)
	}
	found, _, err := s.SearchPeople(ctx, "pacino", 0, 10)
	if err != nil || len(found) != 1 || found[0].Blurhashes[photo] == "" {
		t.Errorf("people found = %+v, %v; want the photo's blurhash", found, err)
	}

	if err := s.SaveIdentity(ctx, item, domain.SourceTMDB, pacino("https://image.tmdb.org/t/p/original/b.jpg"), nil); err != nil {
		t.Fatal(err)
	}
	if person, err = s.Person(ctx, person.ID); err != nil || person.Photo == photo || person.Blurhashes != nil {
		t.Errorf("person = %+v, %v; want a new photo, without the old one's blurhash", person, err)
	}
}
