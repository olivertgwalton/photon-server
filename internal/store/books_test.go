//go:build integration

package store

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"math"
	"slices"
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
	if reach, err := s.SaveReading(ctx, ada.ID, book, locator, 0.42, nil); err != nil || reach != domain.ReachResumable {
		t.Fatalf("reading: %s, %v; want somewhere to resume from", reach, err)
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
	// A place read offline before the book's state last changed is too old to keep.
	yesterday := time.Now().Add(-24 * time.Hour)
	if _, err := s.SaveReading(ctx, ada.ID, book, locator, 0.1, &yesterday); !errors.Is(err, ErrSuperseded) {
		t.Errorf("reading from yesterday: %v, want ErrSuperseded", err)
	}
	if reach, err := s.SaveReading(ctx, ada.ID, book, locator, 1, nil); err != nil || reach != domain.ReachEnd {
		t.Fatalf("reading to the end: %s, %v; want the book read", reach, err)
	}
	if p, err := s.Title(ctx, ada.ID, book); err != nil || p.State.WatchedAt == nil || p.State.Plays != 1 {
		t.Errorf("once read to the end: %+v, %v; want read once", p.State, err)
	}
	if reading, _, err := s.RowPage(ctx, ada.ID, domain.RowContinueReading, Page{Limit: 10}); err != nil || len(reading) != 0 {
		t.Errorf("continue reading once finished: %+v, %v; want none", reading, err)
	}
	if _, err := s.SaveReading(ctx, ada.ID, author, locator, 0.1, nil); !errors.Is(err, ErrNotRead) {
		t.Errorf("reading an author: %v, want ErrNotRead", err)
	}
}

// A refresh of a books library asks its providers of its authors and books again.
func TestABooksLibrarysRefreshAsksAgainOfItsAuthorsAndBooks(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "Books", domain.LibraryBooks, t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	rel := "Earthsea/earthsea.epub"
	if _, err := s.SaveBooksFolder(ctx, lib.ID, "Earthsea", []byte("seen"), []Book{{
		Title: "A Wizard of Earthsea", Author: "Ursula K. Le Guin", Folder: "Earthsea",
		Versions: []Version{{ContentKey: []byte(rel), Parts: []Part{{RelPath: rel, Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{Container: "epub", Size: 1}}}}},
	}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM jobs`); err != nil {
		t.Fatal(err)
	}
	if err := s.RefreshLibrary(ctx, lib.ID, domain.RefreshAll); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM jobs j JOIN items i ON i.id = j.subject
		WHERE j.kind = 'identify' AND i.kind IN ('author', 'book')`).Scan(&n); err != nil || n != 2 {
		t.Errorf("refresh asked of %d authors and books, %v; want 2", n, err)
	}
}

// A search finds music and books as it finds films: an album before the track named as it, which
// it holds, and a book by a word of its title; and a search for books alone finds no album.
func TestASearchFindsMusicAndBooks(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	music, err := s.AddLibrary(ctx, "Music", domain.LibraryMusic, t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	books, err := s.AddLibrary(ctx, "Books", domain.LibraryBooks, t.TempDir()+"/books", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveMusicFolder(ctx, music.ID, "Hurry Up Tomorrow", []byte("seen"), []Album{{
		Title: "Hurry Up Tomorrow", Artist: "The Weeknd", Folder: "Hurry Up Tomorrow",
		Tracks: []Track{track("Hurry Up Tomorrow", 2, 11)},
	}}); err != nil {
		t.Fatal(err)
	}
	rel := "Earthsea/earthsea.epub"
	if _, err := s.SaveBooksFolder(ctx, books.ID, "Earthsea", []byte("seen"), []Book{{
		Title: "A Wizard of Earthsea", Author: "Ursula K. Le Guin", Folder: "Earthsea",
		Versions: []Version{{ContentKey: []byte(rel), Parts: []Part{{RelPath: rel, Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{Container: "epub", Size: 1}}}}},
	}}); err != nil {
		t.Fatal(err)
	}
	found := func(text string, kinds ...domain.ItemKind) []string {
		t.Helper()
		cards, _, err := s.Search(ctx, SearchQuery{Text: text, Kinds: kinds, Limit: 10})
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, c := range cards {
			out = append(out, string(c.Kind)+" "+c.Title)
		}
		return out
	}
	if got, want := found("hurry up"), []string{"album Hurry Up Tomorrow", "track Hurry Up Tomorrow"}; !cmp.Equal(got, want) {
		t.Errorf("hurry up: %v, want %v", got, want)
	}
	if got, want := found("earthsea"), []string{"book A Wizard of Earthsea"}; !cmp.Equal(got, want) {
		t.Errorf("earthsea: %v, want %v", got, want)
	}
	if got, want := found("weeknd"), []string{"artist The Weeknd"}; !cmp.Equal(got, want) {
		t.Errorf("weeknd: %v, want %v", got, want)
	}
	if got := found("hurry", domain.ItemBook); len(got) != 0 {
		t.Errorf("hurry among books: %v, want none", got)
	}
}

// A book is favourited but not watched nor put on a watchlist, so its wall is offered that alone.
func TestABookWallIsOfferedFavouritesAlone(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "Books", domain.LibraryBooks, t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	rel := "Earthsea/earthsea.epub"
	if _, err := s.SaveBooksFolder(ctx, lib.ID, "Earthsea", []byte("seen"), []Book{{
		Title: "A Wizard of Earthsea", Author: "Ursula K. Le Guin", Folder: "Earthsea",
		Versions: []Version{{ContentKey: []byte(rel), Parts: []Part{{RelPath: rel, Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{Container: "epub", Size: 1}}}}},
	}}); err != nil {
		t.Fatal(err)
	}
	facets, err := s.Facets(ctx, []uuid.UUID{lib.ID}, uuid.UUID{}, domain.WallBooks)
	if err != nil || !slices.Equal(facets.Marks, []domain.Mark{domain.MarkFavourite}) {
		t.Errorf("a book wall's marks = %v, %v; want favourites alone", facets.Marks, err)
	}
}
