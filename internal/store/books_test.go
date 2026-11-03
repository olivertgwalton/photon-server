//go:build integration

package store

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"math"
	"testing"
	"time"
	"uuid"

	"github.com/google/go-cmp/cmp"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// A reader's place in a book is kept as its reader gave it, and the book is in Continue Reading
// until it is finished; a title that is no book is not read.
func TestAReadersPlaceInABookIsKept(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "Books", domain.LibraryBooks, t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	rel := "Earthsea/earthsea.epub"
	if _, err := s.SaveBooksFolder(ctx, lib.ID, "Earthsea", []byte("seen"), []Book{{
		Title: "A Wizard of Earthsea", Author: "Ursula K. Le Guin", Series: "Earthsea", SeriesIndex: 1, Folder: "Earthsea",
		Versions: []Version{{ContentKey: []byte(rel), Parts: []Part{{RelPath: rel, Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{Container: "epub", Size: 1}}}}},
	}}); err != nil {
		t.Fatal(err)
	}
	ada := addProfile(t, s, "Ada", domain.RoleAdmin, nil)
	var book, author uuid.UUID
	if err := s.pool.QueryRow(ctx, `SELECT id, parent_id FROM items WHERE kind = 'book'`).Scan(&book, &author); err != nil {
		t.Fatal(err)
	}
	locator := jsontext.Value(`{"href":"/OEBPS/text/c3.xhtml","type":"application/xhtml+xml","locations":{"progression":0.5,"totalProgression":0.42}}`)
	if err := s.SaveReading(ctx, ada.ID, book, locator, 0.42); err != nil {
		t.Fatal(err)
	}
	p, err := s.Title(ctx, ada.ID, book)
	if err != nil {
		t.Fatal(err)
	}
	var gave, kept any
	if err := json.Unmarshal(locator, &gave); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(p.State.Locator, &kept); err != nil {
		t.Fatal(err)
	}
	if p.State.Progression == nil || math.Abs(*p.State.Progression-0.42) > 1e-6 || !cmp.Equal(gave, kept) {
		t.Errorf("state: %v at %s, want 0.42 at the locator given", p.State.Progression, p.State.Locator)
	}
	if p.Author == nil || p.Author.ID != author || p.Series != "Earthsea" || *p.SeriesIndex != 1 || len(p.Versions) != 1 {
		t.Errorf("page: author %+v, series %q %v, %d versions; want Le Guin's Earthsea 1 in one format", p.Author, p.Series, p.SeriesIndex, len(p.Versions))
	}
	reading, _, err := s.RowPage(ctx, ada.ID, domain.RowContinueReading, Page{Limit: 10})
	if err != nil || len(reading) != 1 || reading[0].ID != book || reading[0].Author == nil {
		t.Fatalf("continue reading: %+v, %v; want the book and its author", reading, err)
	}
	if err := s.MarkWatched(ctx, ada.ID, book, nil); err != nil {
		t.Fatal(err)
	}
	if reading, _, err := s.RowPage(ctx, ada.ID, domain.RowContinueReading, Page{Limit: 10}); err != nil || len(reading) != 0 {
		t.Errorf("continue reading once finished: %+v, %v; want none", reading, err)
	}
	if err := s.SaveReading(ctx, ada.ID, author, locator, 0.1); !errors.Is(err, ErrNotRead) {
		t.Errorf("reading an author: %v, want ErrNotRead", err)
	}
}
