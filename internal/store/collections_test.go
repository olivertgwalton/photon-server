//go:build integration

package store

import (
	"errors"
	"slices"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/media"
)

func TestBoxSetsAreMadeFromWhatAProviderSays(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]uuid.UUID{}
	for _, f := range []struct {
		title    string
		released time.Time
	}{{"Aliens", time.Date(1986, 7, 18, 0, 0, 0, 0, time.UTC)}, {"Alien", time.Date(1979, 5, 25, 0, 0, 0, 0, time.UTC)}, {"Heat", time.Date(1995, 12, 15, 0, 0, 0, 0, time.UTC)}} {
		film := Film{Title: f.title, Folder: f.title, Copies: []Copy{{ContentKey: []byte(f.title), Parts: []Part{{
			RelPath: f.title + ".mkv", Size: 1, ModTime: time.Unix(0, 0), Facts: &media.Facts{},
		}}}}}
		if _, err := s.SaveFolder(ctx, lib.ID, f.title, []byte("v1"), []Film{film}, nil); err != nil {
			t.Fatal(err)
		}
		cards, _, err := s.Wall(ctx, lib.ID, WallPage{Sort: domain.SortAdded, Order: domain.Descending, Limit: 1})
		if err != nil {
			t.Fatal(err)
		}
		ids[f.title] = cards[0].ID
		m := domain.Metadata{Title: f.title, ReleaseDate: f.released}
		if f.title != "Heat" {
			m.Collections = []domain.Grouping{{ID: "8091", Title: "Alien Collection", Artwork: []domain.Artwork{{Kind: domain.ArtworkPoster, URL: "https://image.tmdb.org/t/p/original/set.jpg"}}}}
		}
		if err := s.SaveIdentity(ctx, ids[f.title], domain.SourceTMDB, m, nil); err != nil {
			t.Fatal(err)
		}
	}
	shown, total, err := s.Collections(ctx, lib.ID, uuid.UUID{}, 0, 10)
	if err != nil || total != 1 || len(shown) != 1 || shown[0].Title != "Alien Collection" || shown[0].Poster == (uuid.UUID{}) {
		t.Fatalf("collections = %+v of %d, %v; want the one set with its poster", shown, total, err)
	}
	set := shown[0].ID
	members, err := s.Members(ctx, uuid.UUID{}, set)
	if err != nil || len(members) != 2 || members[0].Title != "Alien" || members[1].Title != "Aliens" {
		t.Errorf("members = %+v, %v; want Alien then Aliens, by release", members, err)
	}
	page, err := s.Title(ctx, uuid.UUID{}, ids["Alien"])
	if err != nil || !slices.Equal(page.Collections, []TitleRef{{ID: set, Title: "Alien Collection"}}) {
		t.Errorf("Alien is in %v, %v", page.Collections, err)
	}
	if shown[0].Origin != domain.CollectionTMDB {
		t.Errorf("the set's card says it was made by %q, want tmdb", shown[0].Origin)
	}
	if page, err := s.Title(ctx, uuid.UUID{}, set); err != nil || page.Origin != domain.CollectionTMDB {
		t.Errorf("the set's page says it was made by %q, %v; want tmdb", page.Origin, err)
	}
	if err := s.SetMembers(ctx, set, nil); !errors.Is(err, ErrNotUserCollection) {
		t.Errorf("changing TMDB's set by hand: %v, want ErrNotUserCollection", err)
	}
	if err := s.RemoveCollection(ctx, set); !errors.Is(err, ErrNotUserCollection) {
		t.Errorf("removing TMDB's set by hand: %v, want ErrNotUserCollection", err)
	}
	// Its name and words are an admin's to edit, as any title's, and outlast TMDB saying them again.
	if err := s.EditMetadata(ctx, set, domain.Metadata{Title: "Alien Anthology"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveIdentity(ctx, ids["Alien"], domain.SourceTMDB, domain.Metadata{Title: "Alien", Collections: []domain.Grouping{{ID: "8091", Title: "Alien Collection"}}}, nil); err != nil {
		t.Fatal(err)
	}
	if page, err := s.Title(ctx, uuid.UUID{}, set); err != nil || page.Title != "Alien Anthology" {
		t.Errorf("the renamed set is called %q, %v; want the admin's name", page.Title, err)
	}

	// Aliens is no longer said to be in it: one is no set, and none is gone at the next scan.
	if err := s.SaveIdentity(ctx, ids["Aliens"], domain.SourceTMDB, domain.Metadata{Title: "Aliens"}, nil); err != nil {
		t.Fatal(err)
	}
	if shown, _, _ := s.Collections(ctx, lib.ID, uuid.UUID{}, 0, 10); len(shown) != 0 {
		t.Errorf("with one title left: %+v, want none shown", shown)
	}
	if err := s.SaveIdentity(ctx, ids["Alien"], domain.SourceTMDB, domain.Metadata{Title: "Alien"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishScan(ctx, lib.ID, []string{"Alien", "Aliens", "Heat"}, []string{"Alien.mkv", "Aliens.mkv", "Heat.mkv"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Members(ctx, uuid.UUID{}, set); !errors.Is(err, ErrNotFound) {
		t.Errorf("an emptied set after a scan: %v, want it gone", err)
	}

	// An admin's own, in their order, of any size.
	mine, err := s.AddCollection(ctx, lib.ID, "Favourites of 1979")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetMembers(ctx, mine, []uuid.UUID{ids["Heat"], ids["Alien"]}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetMembers(ctx, mine, []uuid.UUID{ids["Heat"], ids["Heat"]}); !errors.Is(err, ErrNotFound) {
		t.Errorf("a title twice: %v, want ErrNotFound", err)
	}
	if err := s.SetMembers(ctx, mine, []uuid.UUID{uuid.NewV7()}); !errors.Is(err, ErrNotFound) {
		t.Errorf("a title of no library: %v, want ErrNotFound", err)
	}
	if shown, _, err := s.Collections(ctx, lib.ID, uuid.UUID{}, 0, 10); err != nil || len(shown) != 1 || shown[0].Origin != domain.CollectionUser {
		t.Errorf("collections = %+v, %v; want the admin's own, saying so", shown, err)
	}
	members, err = s.Members(ctx, uuid.UUID{}, mine)
	if err != nil || len(members) != 2 || members[0].Title != "Heat" {
		t.Errorf("an admin's set = %+v, %v; want Heat first, as put", members, err)
	}
	viewer, err := s.AddProfile(ctx, "Viewer", domain.RoleMember, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.MarkWatched(ctx, viewer.ID, mine); err != nil {
		t.Fatal(err)
	}
	if watched, _, _ := s.Wall(ctx, lib.ID, WallPage{Profile: viewer.ID, Sort: domain.SortTitle, Limit: 10, Filter: WallFilter{Marks: []domain.Mark{domain.MarkWatched}}}); len(watched) != 2 {
		t.Errorf("after marking the set watched, %d titles are, want both", len(watched))
	}
	if err := s.RemoveCollection(ctx, mine); err != nil {
		t.Fatal(err)
	}
	if page, err := s.Title(ctx, uuid.UUID{}, ids["Heat"]); err != nil || len(page.Collections) != 0 {
		t.Errorf("after removing it, Heat is in %v, %v", page.Collections, err)
	}
}
