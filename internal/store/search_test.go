//go:build integration

package store

import (
	"slices"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
)

func TestSearchMatchesTheStartOfWords(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	films, err := s.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	tv, err := s.AddLibrary(ctx, "TV", domain.LibraryShows, "/srv/tv")
	if err != nil {
		t.Fatal(err)
	}
	add := func(lib uuid.UUID, kind domain.ItemKind, title, original string) {
		t.Helper()
		item := model.Item{
			LibraryID: lib, Kind: kind, Title: title, ScanTitle: title, SortTitle: sortTitle(title), Folder: title,
		}
		if original != "" {
			item.OriginalTitle = &original
		}
		addItem(t, s, item)
	}
	add(films.ID, domain.ItemMovie, "Amélie", "Le Fabuleux Destin d'Amélie Poulain")
	add(films.ID, domain.ItemMovie, "Heat", "")
	add(films.ID, domain.ItemMovie, "Theatre of Blood", "")
	add(films.ID, domain.ItemMovie, "Heat Wave", "")
	add(films.ID, domain.ItemMovie, "The Godfather", "")
	add(tv.ID, domain.ItemShow, "The Heat", "")
	// An episode comes after the films and shows matched as well, but before those matched worse.
	add(tv.ID, domain.ItemEpisode, "Heat Seeker", "")
	search := func(text string, lib uuid.UUID, kinds ...domain.ItemKind) []string {
		t.Helper()
		cards, total, err := s.Search(ctx, SearchQuery{Text: text, Library: lib, Kinds: kinds, Limit: 20})
		if err != nil || int(total) != len(cards) {
			t.Fatal(cards, total, err)
		}
		var titles []string
		for _, c := range cards {
			titles = append(titles, c.Title)
		}
		return titles
	}
	for _, tc := range []struct {
		text  string
		lib   uuid.UUID
		kinds []domain.ItemKind
		want  []string
	}{
		{"heat", uuid.UUID{}, nil, []string{"Heat", "Heat Wave", "Heat Seeker", "The Heat"}},
		{"heat", tv.ID, nil, []string{"Heat Seeker", "The Heat"}},
		{"heat", uuid.UUID{}, []domain.ItemKind{domain.ItemMovie}, []string{"Heat", "Heat Wave"}},
		{"heat", uuid.UUID{}, []domain.ItemKind{domain.ItemShow, domain.ItemEpisode}, []string{"Heat Seeker", "The Heat"}},
		{"AMEL", uuid.UUID{}, nil, []string{"Amélie"}},
		{"destin poul", uuid.UUID{}, nil, []string{"Amélie"}},
		{"eat", uuid.UUID{}, nil, nil},
		{"?!", uuid.UUID{}, nil, nil},
		// A letter wrong still finds the title, once enough is typed to tell.
		{"amalie", uuid.UUID{}, nil, []string{"Amélie"}},
		{"godfater", uuid.UUID{}, nil, []string{"The Godfather"}},
		{"godfater", tv.ID, nil, nil},
		{"godfater", uuid.UUID{}, []domain.ItemKind{domain.ItemShow}, nil},
		{"heta", uuid.UUID{}, nil, nil},
	} {
		if got := search(tc.text, tc.lib, tc.kinds...); !slices.Equal(got, tc.want) {
			t.Errorf("search %q %v: %q, want %q", tc.text, tc.kinds, got, tc.want)
		}
	}
	// A page past the first goes on where it left off and counts every match.
	cards, total, err := s.Search(ctx, SearchQuery{Text: "heat", Offset: 1, Limit: 1})
	if err != nil || total != 4 || len(cards) != 1 || cards[0].Title != "Heat Wave" {
		t.Errorf("the second of four: %+v of %d, %v; want Heat Wave", cards, total, err)
	}
}

// A film in two libraries is found once, in the library added first, unless the profile may see
// only the other.
func TestSearchFindsATitleInSeveralLibrariesOnce(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	part := func(rel string) []Copy {
		return []Copy{{ContentKey: []byte(rel), Parts: []Part{{RelPath: rel, Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{Duration: time.Hour}}}}}
	}
	var heats []uuid.UUID
	var libs []uuid.UUID
	for _, name := range []string{"Films", "4K"} {
		lib, err := s.AddLibrary(ctx, name, domain.LibraryMovies, "/srv/"+name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.SaveFolder(ctx, lib.ID, "Heat", []byte("v"), []Film{{Title: "Heat", Folder: "Heat", Copies: part("Heat/" + name + ".mkv")}}, nil); err != nil {
			t.Fatal(err)
		}
		heat := oneItem(t, s, "title = 'Heat' AND library_id = '"+lib.ID.String()+"'").ID
		if _, err := s.pool.Exec(ctx, `INSERT INTO external_ids (item_id, provider, value, source) VALUES ($1, 'tmdb', '949', 'match')`, heat); err != nil {
			t.Fatal(err)
		}
		heats, libs = append(heats, heat), append(libs, lib.ID)
	}
	if _, err := s.pool.Exec(ctx, `SELECT key_titles($1)`, heats); err != nil {
		t.Fatal(err)
	}
	admin, err := s.AddProfile(ctx, "Oliver", domain.RoleAdmin, "hash", nil)
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.AddProfile(ctx, "Guest", domain.RoleUser, "hash", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetAccess(ctx, other.ID, ProfileAccess{Libraries: []uuid.UUID{libs[1]}}, nil); err != nil {
		t.Fatal(err)
	}
	for profile, want := range map[uuid.UUID]uuid.UUID{admin.ID: heats[0], other.ID: heats[1]} {
		cards, total, err := s.Search(ctx, SearchQuery{Profile: profile, Text: "heat", Limit: 20})
		if err != nil || total != 1 || len(cards) != 1 || cards[0].ID != want {
			t.Errorf("found %+v of %d, %v; want the one Heat %v", cards, total, err, want)
		}
	}
}
