//go:build integration

package store

import (
	"testing"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// What a remote library's search finds is kept under ids that are the same each time it is found,
// with its poster, and leaves out what a library already holds. Opened, it becomes a title of the
// library under its id, matched by the id it was found by; a profile held to an age searches none.
func TestASearchFindsARemoteLibraryTitlesItBecomesAsOpened(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddRemoteLibrary(ctx, "Found", domain.LibraryMovies, Remote{DiscoverSource: domain.SourceTMDB, StreamSource: domain.PluginSource("riven")})
	if err != nil {
		t.Fatal(err)
	}
	films, err := s.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	film := Film{Title: "Heat", Folder: "H", IDs: map[domain.Provider]string{domain.ProviderTMDB: "949"}, Copies: []Copy{{
		ContentKey: []byte("c"), Parts: []Part{{RelPath: "H/heat.mkv", Size: 1, Facts: &domain.Facts{Duration: 1}}},
	}}}
	if _, err := s.SaveFolder(ctx, films.ID, "H", []byte("v1"), []Film{film}, nil); err != nil {
		t.Fatal(err)
	}

	found := []domain.Candidate{
		{ID: "949", Title: "Heat", Year: 1995},
		{ID: "27205", Title: "Inception", Year: 2010, Overview: "Dreams.", Poster: "https://image.tmdb.org/t/p/original/inception.jpg"},
	}
	saved, err := s.SaveDiscoveries(ctx, lib.ID, domain.ItemMovie, domain.ProviderTMDB, found)
	if err != nil || len(saved) != 1 || saved[0].Title != "Inception" {
		t.Fatalf("found %+v, %v; want Inception, not Heat, which a library holds", saved, err)
	}
	again, err := s.SaveDiscoveries(ctx, lib.ID, domain.ItemMovie, domain.ProviderTMDB, found)
	if err != nil || len(again) != 1 || again[0].ID != saved[0].ID {
		t.Errorf("found again as %+v, %v; want the same id", again, err)
	}
	if pic, err := s.Picture(ctx, saved[0].Poster); err != nil || pic.URL != found[1].Poster {
		t.Errorf("its poster is %+v, %v; want TMDB's", pic, err)
	}

	held, err := s.HoldDiscovered(ctx, saved[0].ID)
	if err != nil || !held {
		t.Fatalf("held %v, %v", held, err)
	}
	item := oneItem(t, s, "kind = 'movie' AND library_id = '"+lib.ID.String()+"'")
	ids, err := s.ExternalIDs(ctx, []uuid.UUID{item.ID})
	if err != nil || item.ID != saved[0].ID || item.Title != "Inception" || ids[item.ID][domain.ProviderTMDB] != "27205" {
		t.Errorf("opened, it is %+v with ids %v, %v", item, ids[item.ID], err)
	}
	if held, err := s.HoldDiscovered(ctx, saved[0].ID); err != nil || held {
		t.Errorf("opened again: %v, %v; want it held already", held, err)
	}
	if held, err := s.HoldDiscovered(ctx, uuid.NewV7()); err != nil || held {
		t.Errorf("an id nothing found: %v, %v", held, err)
	}

	if libs, err := s.Discoverable(ctx, uuid.UUID{}); err != nil || len(libs) != 1 || libs[0].Source != domain.SourceTMDB {
		t.Errorf("searched libraries %+v, %v; want Found", libs, err)
	}
	kid, err := s.AddProfile(ctx, "Kid", domain.RoleUser, "hash", nil)
	if err != nil {
		t.Fatal(err)
	}
	twelve := 12
	if err := s.SetAccess(ctx, kid.ID, ProfileAccess{MaxAge: &twelve, Unrated: domain.UnratedBlock}, nil); err != nil {
		t.Fatal(err)
	}
	if libs, err := s.Discoverable(ctx, kid.ID); err != nil || len(libs) != 0 {
		t.Errorf("a profile held to 12 searches %+v, %v; want none", libs, err)
	}
}
