//go:build integration

package store

import (
	"errors"
	"slices"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
)

func TestWallPagesEveryTitleOnce(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// Two titles share a sort title and two share a time added, so the id breaks both ties.
	// Released: a date, a year alone (its first of January), or neither, which goes last.
	date := func(y, m, d int) *time.Time { return new(time.Date(y, time.Month(m), d, 0, 0, 0, 0, time.UTC)) }
	for n, f := range []struct {
		title    string
		year     int
		released *time.Time
	}{
		{"Heat", 1995, date(1995, 12, 15)},
		{"Alien", 1979, nil},
		{"heat", 0, nil},
		{"Brazil", 1985, date(1985, 2, 20)},
		{"Zodiac", 0, date(2007, 3, 2)},
		{"Memento", 2000, date(2000, 9, 5)},
		{"Ran", 1985, nil},
	} {
		item := model.Item{
			LibraryID: lib.ID, Kind: domain.ItemMovie, Title: f.title, ScanTitle: f.title,
			SortTitle: sortTitle(f.title), Folder: f.title, AddedAt: base.Add(time.Duration(n%5) * time.Hour),
			ReleaseDate: f.released,
		}
		if f.year != 0 {
			item.Year = &f.year
		}
		id := addItem(t, s, item)
		if f.title == "Heat" {
			addItem(t, s, model.Item{
				LibraryID: lib.ID, Kind: domain.ItemExtra, ParentID: &id, ExtraKind: new(domain.ExtraTrailer), Title: "Trailer",
				ScanTitle: "Trailer", SortTitle: "trailer", Folder: "Heat",
			})
		}
	}

	for _, sort := range domain.WallSorts() {
		for _, order := range []domain.Order{domain.Ascending, domain.Descending} {
			full, total, err := s.Wall(ctx, []uuid.UUID{lib.ID}, WallPage{Sort: sort, Order: order, Limit: 100})
			if err != nil || total != 7 || len(full) != 7 {
				t.Fatalf("%s %s in one page: %d titles of %d, err %v; want the 7 films", sort, order, len(full), total, err)
			}
			var paged []Card
			for offset := 0; offset < int(total); offset += 3 {
				page, n, err := s.Wall(ctx, []uuid.UUID{lib.ID}, WallPage{Sort: sort, Order: order, Offset: offset, Limit: 3})
				if err != nil || n != total {
					t.Fatalf("from %d: %d of %d, %v", offset, len(page), n, err)
				}
				paged = append(paged, page...)
			}
			ids := func(cs []Card) []uuid.UUID {
				out := make([]uuid.UUID, len(cs))
				for n, c := range cs {
					out[n] = c.ID
				}
				return out
			}
			if !slices.Equal(ids(paged), ids(full)) {
				t.Errorf("%s %s: paging by 3 gave %v, want %v", sort, order, ids(paged), ids(full))
			}
			if sort == domain.SortReleased {
				var titles []string
				for _, c := range full {
					titles = append(titles, c.Title)
				}
				want := []string{"Alien", "Ran", "Brazil", "Heat", "Memento", "Zodiac", "heat"}
				if order == domain.Descending {
					want = []string{"Zodiac", "Memento", "Heat", "Brazil", "Ran", "Alien", "heat"}
				}
				if !slices.Equal(titles, want) {
					t.Errorf("released %s = %q, want %q", order, titles, want)
				}
			}
		}
	}

	if past, total, err := s.Wall(ctx, []uuid.UUID{lib.ID}, WallPage{Sort: domain.SortTitle, Offset: 7, Limit: 3}); err != nil || len(past) != 0 || total != 7 {
		t.Errorf("past the end: %d of %d, %v; want none of 7", len(past), total, err)
	}
	// Alien, Brazil, Heat and heat, Memento, Ran, Zodiac.
	letters, err := s.Letters(ctx, lib.ID, uuid.UUID{}, WallFilter{})
	want := []Letter{{"A", 1}, {"B", 1}, {"H", 2}, {"M", 1}, {"R", 1}, {"Z", 1}}
	if err != nil || !slices.Equal(letters, want) {
		t.Errorf("letters = %v, %v; want %v", letters, err, want)
	}
	if _, err := s.Letters(ctx, uuid.NewV7(), uuid.UUID{}, WallFilter{}); !errors.Is(err, ErrNotFound) {
		t.Errorf("letters of an unknown library: %v, want ErrNotFound", err)
	}
	if _, _, err := s.Wall(ctx, []uuid.UUID{uuid.NewV7()}, WallPage{Sort: domain.SortTitle, Limit: 3}); !errors.Is(err, ErrNotFound) {
		t.Errorf("an unknown library: err = %v, want ErrNotFound", err)
	}
}

// Numbers in titles are read as numbers, as Jellyfin sorts them.
func TestTitlesSortTheirNumbersAsNumbers(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	for _, title := range []string{"1917", "The Age of Adaline", "13 Going on 30", "2 Fast 2 Furious", "Alien", "21 Jump Street"} {
		addItem(t, s, model.Item{
			LibraryID: lib.ID, Kind: domain.ItemMovie, Title: title, ScanTitle: title,
			SortTitle: sortTitle(title), Folder: title,
		})
	}
	page, _, err := s.Wall(ctx, []uuid.UUID{lib.ID}, WallPage{Sort: domain.SortTitle, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	var titles []string
	for _, c := range page {
		titles = append(titles, c.Title)
	}
	want := []string{"2 Fast 2 Furious", "13 Going on 30", "21 Jump Street", "1917", "The Age of Adaline", "Alien"}
	if !slices.Equal(titles, want) {
		t.Errorf("by title = %q, want %q", titles, want)
	}
}

// Libraries are read as one wall, their titles sorted and paged together, as an app asks for
// every library's films and shows at once.
func TestOneWallOfSeveralLibraries(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	admin, _ := homeLibraries(t, s, []string{"Alien", "Heat"}, []string{"Fargo"})
	libs, err := s.LibrariesSeen(ctx, admin)
	if err != nil || len(libs) != 2 {
		t.Fatalf("libraries %v, %v", libs, err)
	}
	ids := []uuid.UUID{libs[0].ID, libs[1].ID}
	titles := func(offset, limit int) ([]string, int64) {
		t.Helper()
		cards, total, err := s.Wall(ctx, ids, WallPage{Profile: admin, Sort: domain.SortTitle, Offset: offset, Limit: limit})
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, c := range cards {
			out = append(out, c.Title)
		}
		return out, total
	}
	if got, total := titles(0, 10); !slices.Equal(got, []string{"Alien", "Fargo", "Heat"}) || total != 3 {
		t.Errorf("both libraries: %v of %d", got, total)
	}
	if got, _ := titles(1, 1); !slices.Equal(got, []string{"Fargo"}) {
		t.Errorf("the second title of both: %v", got)
	}
}

// Titles asked for by id come in the order asked, each once, and only those the profile may see.
func TestCardsAreTheTitlesAskedForByID(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	admin, kid := homeLibraries(t, s, []string{"Alien", "Heat"}, []string{"Fargo"})
	heat, fargo := oneItem(t, s, "title = 'Heat'").ID, oneItem(t, s, "kind = 'show'").ID
	episode := oneItem(t, s, "kind = 'episode'").ID
	titles := func(profile uuid.UUID, ids ...uuid.UUID) []string {
		t.Helper()
		cards, err := s.Cards(ctx, profile, ids)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, c := range cards {
			out = append(out, c.Title)
		}
		return out
	}
	if got := titles(admin, fargo, uuid.NewV7(), heat, fargo, episode); !slices.Equal(got, []string{"Fargo", "Heat", "Fargo S1E1"}) {
		t.Errorf("by id: %v", got)
	}
	if got := titles(kid, fargo, heat); !slices.Equal(got, []string{"Heat"}) {
		t.Errorf("for a profile without the shows: %v, want the film alone", got)
	}
}
