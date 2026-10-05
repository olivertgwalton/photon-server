//go:build integration

package store

import (
	"errors"
	"testing"
	"uuid"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

func TestLibraries(t *testing.T) {
	s := migrated(t)
	films, err := s.AddLibrary(t.Context(), "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	if films.ID == (uuid.UUID{}) {
		t.Error("the new library has no id")
	}
	if _, err := s.AddLibrary(t.Context(), "Television", domain.LibraryShows, "/srv/tv"); err != nil {
		t.Fatal(err)
	}
	for _, dup := range [][2]string{{"Films", "/srv/other"}, {"Other", "/srv/films"}} {
		if _, err := s.AddLibrary(t.Context(), dup[0], domain.LibraryMovies, dup[1]); !errors.Is(err, ErrLibraryExists) {
			t.Errorf("adding %q at %q: err = %v, want %v", dup[0], dup[1], err, ErrLibraryExists)
		}
	}

	got, err := s.Libraries(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	want := []domain.Library{
		{Name: "Films", Kind: domain.LibraryMovies, Root: "/srv/films"},
		{Name: "Television", Kind: domain.LibraryShows, Root: "/srv/tv"},
	}
	if diff := cmp.Diff(want, got, cmpopts.IgnoreFields(domain.Library{}, "ID")); diff != "" {
		t.Errorf("libraries (-want +got):\n%s", diff)
	}
}
