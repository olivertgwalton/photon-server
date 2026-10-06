//go:build integration

package store

import (
	"slices"
	"testing"
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
