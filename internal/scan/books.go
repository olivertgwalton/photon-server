package scan

import (
	"cmp"
	"context"
	"errors"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/ebook"
	"github.com/olivertgwalton/photon-server/internal/library"
	"github.com/olivertgwalton/photon-server/internal/media"
	"github.com/olivertgwalton/photon-server/internal/naming"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// unknownAuthor is the author of a book that names none, in a folder that names none either.
const unknownAuthor = "Unknown Author"

// edition is what a store's title of an audiobook adds to its book's, "(Unabridged)".
var edition = regexp.MustCompile(`(?i)\s*[\[(](un)?abridged[\])]\s*$`)

// bookPlan is a book a folder holds: what its files say of it, its versions, each an ebook's file
// or an audiobook's sound files, and the cue sheet of an audiobook in one file.
type bookPlan struct {
	book     store.Book
	versions []versionPlan
	cue      string
}

// planBooks reads a folder of a books library: its sound files are one audiobook, in the order
// their tags and names give, and each ebook is a book of its own; an ebook and the audiobook of
// the same title are one book's versions. It answers why each file it could not read was not.
func planBooks(root string, f library.Folder) ([]bookPlan, map[string]error) {
	unread := map[string]error{}
	var plans []bookPlan
	if audio, ok := planAudiobook(root, f, unread); ok {
		plans = append(plans, audio)
	}
	for _, file := range f.Files {
		if !naming.IsBook(file.Name) {
			continue
		}
		rel := path.Join(f.Path, file.Name)
		var info ebook.Info
		if !strings.EqualFold(path.Ext(file.Name), ".pdf") {
			var err error
			if info, err = ebook.Read(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
				unread[rel] = err
				continue
			}
		}
		b := store.Book{
			Title: cmp.Or(info.Title, naming.CleanName(stem(file.Name)).Title), Series: info.Series, SeriesIndex: info.SeriesIndex,
			Year: info.Year, Released: info.Released, Overview: info.Description, Genres: info.Genres, Folder: f.Path,
		}
		if len(info.Authors) > 0 {
			b.Author = info.Authors[0]
		}
		if info.Cover != "" {
			b.Artwork = []domain.Artwork{{Kind: domain.ArtworkPoster, Path: rel}}
		}
		v := versionPlan{parts: []library.File{file}}
		if n := slices.IndexFunc(plans, func(p bookPlan) bool { return strings.EqualFold(p.book.Title, b.Title) }); n >= 0 {
			plans[n].versions = append(plans[n].versions, v)
			plans[n].book.Fill(b)
			continue
		}
		plans = append(plans, bookPlan{book: b, versions: []versionPlan{v}})
	}
	for i := range plans {
		plans[i].book.Author = cmp.Or(plans[i].book.Author, folderAuthor(f.Path), unknownAuthor)
	}
	return plans, unread
}

// planAudiobook is the audiobook a folder's sound files are, if it has any: one version of them
// all, by disc, then track, then name, as Audiobookshelf orders a book's files, described by the
// first's tags.
func planAudiobook(root string, f library.Folder, unread map[string]error) (bookPlan, bool) {
	type part struct {
		file library.File
		tags media.Tags
	}
	var parts []part
	for _, file := range f.Files {
		if !naming.IsAudio(file.Name) {
			continue
		}
		rel := path.Join(f.Path, file.Name)
		tags, err := media.ReadTags(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			unread[rel] = err
			continue
		}
		parts = append(parts, part{file, tags})
	}
	if len(parts) == 0 {
		return bookPlan{}, false
	}
	slices.SortStableFunc(parts, func(a, b part) int {
		return cmp.Or(cmp.Compare(a.tags.Disc, b.tags.Disc), cmp.Compare(a.tags.Track, b.tags.Track), naturally(a.file.Name, b.file.Name))
	})
	t := parts[0].tags
	b := store.Book{
		Title:  edition.ReplaceAllString(cmp.Or(t.Album, naming.CleanName(path.Base(f.Path)).Title), ""),
		Series: t.Series, SeriesIndex: seriesIndex(t.SeriesPart), Year: t.Year, Released: t.Released, Overview: t.Comment,
		Genres: t.Genres, Folder: f.Path,
	}
	authors := t.AlbumArtists
	if len(authors) == 0 {
		authors = t.Artists
	}
	if len(authors) > 0 {
		b.Author = authors[0]
	}
	if t.Pictured {
		b.Artwork = []domain.Artwork{{Kind: domain.ArtworkPoster, Path: path.Join(f.Path, parts[0].file.Name)}}
	}
	v := versionPlan{}
	for _, p := range parts {
		v.parts = append(v.parts, p.file)
	}
	plan := bookPlan{book: b, versions: []versionPlan{v}}
	if len(parts) == 1 {
		plan.cue = t.CueSheet
	}
	return plan, true
}

// folderAuthor is the author a folder's place names, as Audiobookshelf's Author/Series/Book: its
// first folder, for a book a folder or more under it.
func folderAuthor(folder string) string {
	first, rest, nested := strings.Cut(folder, "/")
	if folder == "." || !nested || rest == "" {
		return ""
	}
	return first
}

func seriesIndex(s string) float64 {
	n, err := strconv.ParseFloat(strings.TrimSpace(strings.SplitN(s, "/", 2)[0]), 64)
	if err != nil {
		return 0
	}
	return n
}

// naturally orders names by the numbers in them as numbers, "Part 2" before "Part 10".
func naturally(a, b string) int {
	for a != "" && b != "" {
		da, db := leadingDigits(a), leadingDigits(b)
		if da != "" && db != "" {
			if c := cmp.Compare(len(strings.TrimLeft(da, "0")), len(strings.TrimLeft(db, "0"))); c != 0 {
				return c
			}
			if c := strings.Compare(strings.TrimLeft(da, "0"), strings.TrimLeft(db, "0")); c != 0 {
				return c
			}
			a, b = a[len(da):], b[len(db):]
			continue
		}
		if c := cmp.Compare(strings.ToLower(a[:1]), strings.ToLower(b[:1])); c != 0 {
			return c
		}
		a, b = a[1:], b[1:]
	}
	return cmp.Compare(len(a), len(b))
}

func leadingDigits(s string) string {
	end := strings.IndexFunc(s, func(r rune) bool { return r < '0' || r > '9' })
	if end < 0 {
		return s
	}
	return s[:end]
}

// saveBooks reads a folder of a books library. The folder's own pictures are its book's where it
// holds one, before any cover its files hold.
func (s *Scanner) saveBooks(ctx context.Context, r reading, folder library.Folder) (store.Saved, error) {
	lib := r.run.lib
	plans, unread := planBooks(lib.Root, folder)
	for rel, err := range unread {
		if errors.Is(err, media.ErrNotTagged) || errors.Is(err, ebook.ErrUnsafe) {
			s.skip(ctx, r.run, rel, err)
		} else {
			s.unreadable(ctx, r, rel, err)
		}
	}
	read, err := s.readVersions(ctx, r, plansOf(plans, func(b bookPlan) []versionPlan { return b.versions }))
	if err != nil {
		return store.Saved{}, err
	}
	var books []store.Book
	for i, p := range plans {
		if len(read[i]) == 0 {
			continue
		}
		b := p.book
		b.Versions = read[i]
		if facts := b.Versions[0].Parts[0].Facts; p.cue != "" && facts != nil && len(facts.Chapters) == 0 {
			facts.Chapters = media.Chapters(p.cue, facts.Duration)
		}
		books = append(books, b)
	}
	if len(books) == 1 {
		pics := picturesIn(lib.Root, folder.Path, fileNames(folder))
		books[0].Artwork = append(pics.own, books[0].Artwork...)
	}
	for i := range books {
		for j, p := range books[i].Artwork {
			if p.Blurhash == "" {
				books[i].Artwork[j] = picture(lib.Root, p.Path, p.Kind)
			}
		}
	}
	r.run.saving.Lock()
	defer r.run.saving.Unlock()
	return s.store.SaveBooksFolder(ctx, lib.ID, folder.Path, r.fingerprint(), books)
}
