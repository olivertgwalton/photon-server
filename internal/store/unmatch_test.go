//go:build integration

package store

import (
	"testing"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

func TestAnUnmatchedFilmKeepsOnlyItsOwnWordsUntilItIsFixed(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveFolder(ctx, lib.ID, "jaws 1975", []byte("v1"), []Film{{Title: "jaws 1975", Folder: "jaws 1975"}}, nil); err != nil {
		t.Fatal(err)
	}
	id := oneItem(t, s, "true").ID
	if err := s.SaveIdentity(ctx, id, domain.SourceTMDB, domain.Metadata{
		Title: "Jaws", Overview: "A shark.", Genres: []string{"Thriller"},
		IDs:     map[domain.Provider]string{domain.ProviderTMDB: "578"},
		Ratings: []domain.Rating{{Site: domain.SiteTMDB, Score: 76, Votes: 10000}},
		Credits: []domain.Credit{{Name: "Roy Scheider", IDs: map[domain.Provider]string{domain.ProviderTMDB: "6355"}, Kind: domain.CreditActor}},
		Artwork: []domain.Artwork{{Kind: domain.ArtworkPoster, URL: "https://image.tmdb.org/t/p/original/jaws.jpg"}},
	}, nil); err != nil {
		t.Fatal(err)
	}
	// An admin's own words are no provider's.
	if err := s.EditMetadata(ctx, id, domain.Metadata{Tagline: "You'll never go in the water again."}); err != nil {
		t.Fatal(err)
	}

	if err := s.Unmatch(ctx, id); err != nil {
		t.Fatal(err)
	}
	page, err := s.Title(ctx, uuid.UUID{}, id)
	if err != nil {
		t.Fatal(err)
	}
	if page.Title != "jaws 1975" || page.Overview != "" || len(page.Genres) != 0 || page.Tagline == "" {
		t.Errorf("unmatched: %q, %q, %v, tagline %q; want the file's name, no provider's words, the admin's tagline",
			page.Title, page.Overview, page.Genres, page.Tagline)
	}
	if len(page.IDs) != 0 || len(page.Ratings) != 0 || len(page.Credits) != 0 || len(page.Artwork) != 0 {
		t.Errorf("unmatched: ids %v, ratings %v, credits %v, artwork %v; want none of a provider's",
			page.IDs, page.Ratings, page.Credits, page.Artwork)
	}
	// Held: identify asks no provider about it, however its library refreshes.
	if _, ok, err := s.IdentifySubject(ctx, id); err != nil || ok {
		t.Errorf("an unmatched film is offered to identify: %v, %v", ok, err)
	}
	if err := s.Unmatch(ctx, oneItem(t, s, "true").ID); err != nil {
		t.Errorf("unmatching again: %v", err)
	}

	// Fixing its match releases it.
	if err := s.PinMatch(ctx, id, domain.ProviderTMDB, "578"); err != nil {
		t.Fatal(err)
	}
	if sub, ok, err := s.IdentifySubject(ctx, id); err != nil || !ok || sub.IDs[domain.ProviderTMDB] != "578" {
		t.Errorf("after its match is fixed: %+v, %v, %v; want it asked of TMDB by its id", sub, ok, err)
	}
}
