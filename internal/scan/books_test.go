//go:build integration

package scan

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/testtool"
)

// epub writes an EPUB at rel whose package says metadata.
func (f *fixture) epub(rel, metadata string, cover []byte) {
	f.t.Helper()
	p := filepath.Join(f.root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		f.t.Fatal(err)
	}
	testtool.EPUB(f.t, p, metadata, cover)
}

// books is the library's books, "author / title [series index] formats", in order.
func (f *fixture) books() string {
	f.t.Helper()
	rows, err := f.db.Query(f.t.Context(), `
		SELECT a.title || ' / ' || b.title || coalesce(' [' || b.series || ' ' || b.series_index || ']', '') || ' ' ||
			(SELECT string_agg(v.container || ':' || (SELECT count(*) FROM parts p WHERE p.version_id = v.id), ',' ORDER BY v.container)
				FROM versions v WHERE v.item_id = b.id)
		FROM items b JOIN items a ON a.id = b.parent_id WHERE b.kind = 'book' ORDER BY 1`)
	if err != nil {
		f.t.Fatal(err)
	}
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			f.t.Fatal(err)
		}
		out = append(out, s)
	}
	return strings.Join(out, "\n")
}

// A folder's sound files are one audiobook, in their discs' order, and its EPUB of the same title
// another version of the same book, under the author and series the files name.
func TestAnAudiobookAndItsEPUBAreOneBook(t *testing.T) {
	f := newFixture(t, domain.LibraryBooks)
	dir := "Death Comes Silently/"
	for _, disc := range []string{"3", "1", "2"} {
		f.track(dir+"Death Comes Silently "+disc+" of 3.mp3", "ALBUM=Death Comes Silently (Unabridged)", "ALBUMARTIST=Carolyn G Hart",
			"DISCNUMBER="+disc+"/3", "TRACKNUMBER=1", "SERIES=Death on Demand", "PART=22")
	}
	f.epub(dir+"death-comes-silently.epub", `<dc:title>Death Comes Silently</dc:title><dc:creator>Carolyn G. Hart</dc:creator>`, []byte("cover"))
	f.scan()
	if got, want := f.books(), "Carolyn G Hart / Death Comes Silently [Death on Demand 22] epub:1,flac:3"; got != want {
		t.Errorf("books: %s, want %s", got, want)
	}
	if n := f.count(`SELECT count(*) FROM parts p JOIN part_files pf ON pf.part_id = p.id
		WHERE pf.rel_path LIKE '%1 of 3.mp3' AND p.idx = 0`); n != 1 {
		t.Error("the audiobook's first disc is not its first part")
	}
	if n := f.count(`SELECT count(*) FROM artwork a JOIN items b ON b.id = a.item_id WHERE b.kind = 'book' AND a.place LIKE '%.epub'`); n != 1 {
		t.Error("the book does not wear the cover its EPUB holds")
	}
}

// A book in an author's folder whose file names no author is that author's; an audiobook in one
// file is read by the chapters its cue sheet marks.
func TestABooksAuthorIsItsFolderAndAnAudiobooksChaptersItsCueSheet(t *testing.T) {
	f := newFixture(t, domain.LibraryBooks)
	f.epub("Ursula K. Le Guin/A Wizard of Earthsea/earthsea.epub", `<dc:title>A Wizard of Earthsea</dc:title>`, nil)
	f.track("Deathly Hallows/Deathly Hallows.mp3", "ALBUM=Harry Potter and the Deathly Hallows", "ALBUMARTIST=J.K. Rowling",
		"CUESHEET=TRACK 1 AUDIO\n  TITLE \"Opening Credits\"\n  INDEX 01 0:00:00\nTRACK 2 AUDIO\n  TITLE \"The Dark Lord Ascending\"\n  INDEX 01 1:03:39\n")
	f.scan()
	want := "J.K. Rowling / Harry Potter and the Deathly Hallows flac:1\nUrsula K. Le Guin / A Wizard of Earthsea epub:1"
	if got := f.books(); got != want {
		t.Errorf("books:\n%s\nwant:\n%s", got, want)
	}
	if n := f.count(`SELECT count(*) FROM chapters WHERE title IN ('Opening Credits', 'The Dark Lord Ascending')`); n != 2 {
		t.Errorf("%d chapters from the cue sheet, want 2", n)
	}
}
