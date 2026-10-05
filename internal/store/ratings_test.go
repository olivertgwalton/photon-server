//go:build integration

package store

import (
	"slices"
	"testing"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

func TestATitleShowsEachSitesRatingFromItsBestSource(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetLibrary(ctx, lib.ID, LibraryChange{Sources: []domain.FieldSource{domain.SourceMDBList, domain.SourceTMDB}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveFolder(ctx, lib.ID, "Jaws", []byte("v1"), []Film{{Title: "Jaws", Folder: "Jaws"}}, nil); err != nil {
		t.Fatal(err)
	}
	item, err := s.q.Item.WithContext(ctx).Take()
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.UUID(item.ID)
	// TMDB's own score, and MDBList's of TMDB and IMDb: MDBList is ranked first in this library.
	if err := s.SaveIdentity(ctx, id, domain.SourceTMDB, domain.Metadata{Title: "Jaws", Ratings: []domain.Rating{
		{Site: domain.SiteTMDB, Score: 76, Votes: 10000},
	}}, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveRatings(ctx, id, domain.SourceMDBList, []domain.Rating{
		{Site: domain.SiteTMDB, Score: 77, Votes: 10114}, {Site: domain.SiteRottenTomatoes, Score: 97}, {Site: domain.SiteIMDb, Score: 81, Votes: 673852},
	}); err != nil {
		t.Fatal(err)
	}
	page, err := s.Title(ctx, uuid.UUID{}, id)
	if err != nil {
		t.Fatal(err)
	}
	want := []RatingRef{{domain.SiteIMDb, 81, 673852}, {domain.SiteTMDB, 77, 10114}, {domain.SiteRottenTomatoes, 97, 0}}
	if !slices.Equal(page.Ratings, want) {
		t.Errorf("ratings = %v, want %v", page.Ratings, want)
	}
	// Asked again, MDBList has nothing: TMDB's own score stands.
	if err := s.SaveRatings(ctx, id, domain.SourceMDBList, nil); err != nil {
		t.Fatal(err)
	}
	if page, err = s.Title(ctx, uuid.UUID{}, id); err != nil || !slices.Equal(page.Ratings, []RatingRef{{domain.SiteTMDB, 76, 10000}}) {
		t.Errorf("after MDBList forgets it: %v, %v; want TMDB's own", page.Ratings, err)
	}
}

func TestAProvidersSettingsAreSetAndCleared(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	if got, err := s.ProviderSettings(ctx, domain.SourceMDBList); err != nil || len(got) != 0 {
		t.Fatalf("before any: %v, %v", got, err)
	}
	if err := s.SetProviderSettings(ctx, domain.SourceMDBList, map[string]string{"api_key": "k1", "other": "x"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetProviderSettings(ctx, domain.SourceMDBList, map[string]string{"other": ""}); err != nil {
		t.Fatal(err)
	}
	if got, err := s.ProviderSettings(ctx, domain.SourceMDBList); err != nil || len(got) != 1 || got["api_key"] != "k1" {
		t.Errorf("after clearing one: %v, %v; want the key alone", got, err)
	}
}
