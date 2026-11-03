package ebook

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/olivertgwalton/photon-server/internal/testtool"
)

// An EPUB 3 says its title, authors (not its illustrator), series and place in it as Calibre writes
// them, its date, ISBN and subjects, and the cover its manifest marks.
func TestAnEPUBSaysWhatItIs(t *testing.T) {
	name := filepath.Join(t.TempDir(), "book.epub")
	testtool.EPUB(t, name, `
		<dc:title>Harry Potter and the Deathly Hallows</dc:title>
		<dc:creator id="a1">J.K. Rowling</dc:creator><meta refines="#a1" property="role">aut</meta>
		<dc:creator id="i1">Mary GrandPré</dc:creator><meta refines="#i1" property="role">ill</meta>
		<dc:date>2007-07-21</dc:date>
		<dc:identifier>urn:isbn:978-0-545-01022-1</dc:identifier>
		<dc:subject>Fantasy</dc:subject>
		<dc:publisher>Bloomsbury</dc:publisher>
		<meta name="calibre:series" content="Harry Potter"/><meta name="calibre:series_index" content="7"/>`, []byte("PNG"))
	got, err := Read(name)
	if err != nil {
		t.Fatal(err)
	}
	want := Info{
		Title: "Harry Potter and the Deathly Hallows", Authors: []string{"J.K. Rowling"}, Series: "Harry Potter", SeriesIndex: 7,
		Released: time.Date(2007, 7, 21, 0, 0, 0, 0, time.UTC), Year: 2007, Publisher: "Bloomsbury", Genres: []string{"Fantasy"},
		ISBN: "9780545010221", Cover: "OEBPS/images/cover.png",
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("(-want +got):\n%s", diff)
	}
	if cover, err := Cover(name, got.Cover); err != nil || string(cover) != "PNG" {
		t.Errorf("cover: %q, %v", cover, err)
	}
}

// An EPUB 3 collection names a series too, and its group position the book's place.
func TestAnEPUB3CollectionIsItsSeries(t *testing.T) {
	name := filepath.Join(t.TempDir(), "book.epub")
	testtool.EPUB(t, name, `<dc:title>Gideon the Ninth</dc:title><dc:creator>Tamsyn Muir</dc:creator>
		<meta property="belongs-to-collection" id="c">The Locked Tomb</meta><meta refines="#c" property="group-position">1.5</meta>`, nil)
	got, err := Read(name)
	if err != nil || got.Series != "The Locked Tomb" || got.SeriesIndex != 1.5 || !cmp.Equal(got.Authors, []string{"Tamsyn Muir"}) {
		t.Errorf("%+v, %v; want The Locked Tomb 1.5 by Tamsyn Muir", got, err)
	}
}

// A comic archive says what its ComicInfo.xml does, and its first page is its cover; one with none
// is its pages alone.
func TestAComicSaysWhatItsComicInfoDoes(t *testing.T) {
	dir := t.TempDir()
	with, without := filepath.Join(dir, "with.cbz"), filepath.Join(dir, "without.cbz")
	testtool.Zip(t, with, map[string]string{
		"002.jpg": "two", "001.jpg": "one", "ComicInfo.xml": `<ComicInfo><Title>Asylum</Title><Series>Saga</Series><Number>3</Number>
			<Writer>Brian K. Vaughan, Fiona Staples</Writer><Year>2012</Year><Month>5</Month><Day>2</Day><Genre>Science Fiction</Genre></ComicInfo>`,
	})
	testtool.Zip(t, without, map[string]string{"b.png": "b", "a.png": "a", ".hidden.png": "x"})
	got, err := Read(with)
	want := Info{
		Title: "Asylum", Series: "Saga", SeriesIndex: 3, Authors: []string{"Brian K. Vaughan", "Fiona Staples"},
		Released: time.Date(2012, 5, 2, 0, 0, 0, 0, time.UTC), Year: 2012, Genres: []string{"Science Fiction"}, Cover: "001.jpg",
	}
	if diff := cmp.Diff(want, got); err != nil || diff != "" {
		t.Errorf("%v (-want +got):\n%s", err, diff)
	}
	if got, err := Read(without); err != nil || got.Cover != "a.png" || got.Title != "" {
		t.Errorf("%+v, %v; want its first page as its cover and nothing else", got, err)
	}
}

// An archive holding a name that climbs out of it is refused, and a document too long to be a
// package document is not read.
func TestAnUnsafeArchiveIsRefused(t *testing.T) {
	dir := t.TempDir()
	climbs, long := filepath.Join(dir, "climbs.cbz"), filepath.Join(dir, "long.epub")
	testtool.Zip(t, climbs, map[string]string{"../../etc/passwd": "root", "001.jpg": "one"})
	if _, err := Read(climbs); err == nil || !strings.Contains(err.Error(), "no reader should open") {
		t.Errorf("an archive climbing out of itself: %v, want refused", err)
	}
	testtool.EPUB(t, long, "<dc:title>"+strings.Repeat("a", maxDocument)+"</dc:title>", nil)
	if _, err := Read(long); err == nil {
		t.Error("a package document over the limit was read")
	}
}
