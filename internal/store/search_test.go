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
		item := &model.Item{
			LibraryID: model.UUID(lib), Kind: kind, Title: title, ScanTitle: title, SortTitle: sortTitle(title), Folder: title,
		}
		if original != "" {
			item.OriginalTitle = &original
		}
		if err := s.q.Item.WithContext(ctx).Create(item); err != nil {
			t.Fatal(err)
		}
	}
	add(films.ID, domain.ItemMovie, "Amélie", "Le Fabuleux Destin d'Amélie Poulain")
	add(films.ID, domain.ItemMovie, "Heat", "")
	add(films.ID, domain.ItemMovie, "Theatre of Blood", "")
	add(films.ID, domain.ItemMovie, "Heat Wave", "")
	add(tv.ID, domain.ItemShow, "The Heat", "")
	// An episode comes after the films and shows matched as well, but before those matched worse.
	add(tv.ID, domain.ItemEpisode, "Heat Seeker", "")
	search := func(text string, lib uuid.UUID) []string {
		t.Helper()
		cards, total, err := s.Search(ctx, SearchQuery{Text: text, Library: lib, Limit: 20})
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
		text string
		lib  uuid.UUID
		want []string
	}{
		{"heat", uuid.UUID{}, []string{"Heat", "Heat Wave", "Heat Seeker", "The Heat"}},
		{"heat", tv.ID, []string{"Heat Seeker", "The Heat"}},
		{"AMEL", uuid.UUID{}, []string{"Amélie"}},
		{"destin poul", uuid.UUID{}, []string{"Amélie"}},
		{"eat", uuid.UUID{}, nil},
		{"?!", uuid.UUID{}, nil},
	} {
		if got := search(tc.text, tc.lib); !slices.Equal(got, tc.want) {
			t.Errorf("search %q: %q, want %q", tc.text, got, tc.want)
		}
	}
	// A page past the first goes on where it left off and counts every match.
	cards, total, err := s.Search(ctx, SearchQuery{Text: "heat", Offset: 1, Limit: 1})
	if err != nil || total != 4 || len(cards) != 1 || cards[0].Title != "Heat Wave" {
		t.Errorf("the second of four: %+v of %d, %v; want Heat Wave", cards, total, err)
	}
}
