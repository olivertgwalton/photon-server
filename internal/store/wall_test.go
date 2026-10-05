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
	for n, title := range []string{"Heat", "Alien", "heat", "Brazil", "Zodiac", "Memento", "Ran"} {
		added := base.Add(time.Duration(n%5) * time.Hour)
		err := s.q.Item.WithContext(ctx).Create(&model.Item{
			LibraryID: model.UUID(lib.ID), Kind: domain.ItemMovie, Title: title, ScanTitle: title,
			SortTitle: sortTitle(title), Folder: title, AddedAt: added,
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	all, err := s.q.Item.WithContext(ctx).Find()
	if err != nil {
		t.Fatal(err)
	}
	parent := all[0].ID
	if err := s.q.Item.WithContext(ctx).Create(&model.Item{
		LibraryID: model.UUID(lib.ID), Kind: domain.ItemExtra, ParentID: &parent, ExtraKind: new(domain.ExtraTrailer), Title: "Trailer",
		ScanTitle: "Trailer", SortTitle: "trailer", Folder: "Heat",
	}); err != nil {
		t.Fatal(err)
	}

	for _, sort := range domain.WallSorts() {
		for _, order := range []domain.Order{domain.Ascending, domain.Descending} {
			full, next, err := s.Wall(ctx, lib.ID, WallPage{Sort: sort, Order: order, Limit: 100})
			if err != nil || next != "" || len(full) != 7 {
				t.Fatalf("%s %s in one page: %d titles, next %q, err %v; want the 7 films", sort, order, len(full), next, err)
			}
			var paged []Card
			after := ""
			for {
				page, next, err := s.Wall(ctx, lib.ID, WallPage{Sort: sort, Order: order, After: after, Limit: 3})
				if err != nil {
					t.Fatal(err)
				}
				paged = append(paged, page...)
				if next == "" {
					break
				}
				after = next
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
		}
	}

	_, next, err := s.Wall(ctx, lib.ID, WallPage{Sort: domain.SortTitle, Order: domain.Ascending, Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Wall(ctx, lib.ID, WallPage{Sort: domain.SortAdded, Order: domain.Descending, After: next, Limit: 3}); !errors.Is(err, ErrBadCursor) {
		t.Errorf("a title cursor on the added order: err = %v, want ErrBadCursor", err)
	}
	if _, _, err := s.Wall(ctx, uuid.NewV7(), WallPage{Sort: domain.SortTitle, Limit: 3}); !errors.Is(err, ErrNotFound) {
		t.Errorf("an unknown library: err = %v, want ErrNotFound", err)
	}
}
