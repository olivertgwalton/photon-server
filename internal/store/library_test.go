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
		{Name: "Films", Kind: domain.LibraryMovies, Root: "/srv/films", Sources: domain.DefaultSources(), RemoteExtras: []domain.ExtraKind{domain.ExtraBehindTheScenes, domain.ExtraFeaturette, domain.ExtraTrailer}, Monitor: domain.MonitorRealtime},
		{Name: "Television", Kind: domain.LibraryShows, Root: "/srv/tv", Sources: domain.DefaultSources(), RemoteExtras: []domain.ExtraKind{domain.ExtraBehindTheScenes, domain.ExtraFeaturette, domain.ExtraTrailer}, Monitor: domain.MonitorRealtime},
	}
	if diff := cmp.Diff(want, got, cmpopts.IgnoreFields(domain.Library{}, "ID")); diff != "" {
		t.Errorf("libraries (-want +got):\n%s", diff)
	}
}

func TestALibraryIsRenamedAndRemoved(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	films, err := s.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddLibrary(ctx, "Television", domain.LibraryShows, "/srv/tv"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetLibrary(ctx, films.ID, LibraryChange{Name: "Television"}); !errors.Is(err, ErrLibraryExists) {
		t.Errorf("renaming onto another's name: %v, want %v", err, ErrLibraryExists)
	}
	if err := s.SetLibrary(ctx, films.ID, LibraryChange{Name: "Movies", Monitor: domain.MonitorOff}); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Library(ctx, films.ID); err != nil || got.Name != "Movies" || got.Monitor != domain.MonitorOff {
		t.Errorf("after renaming: %+v, %v", got, err)
	}
	if err := s.RemoveLibrary(ctx, films.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Library(ctx, films.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("after removing: %v, want %v", err, ErrNotFound)
	}
	if err := s.RemoveLibrary(ctx, films.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("removing it again: %v, want %v", err, ErrNotFound)
	}
}
