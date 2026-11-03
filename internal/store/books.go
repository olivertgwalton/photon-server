package store

import (
	"cmp"
	"context"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"path"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
)

// Book is a book as a folder of a books library holds it: what its files say of it, the author
// they name, and its versions, each an ebook's file or an audiobook's.
type Book struct {
	Title, SortTitle string
	Author           string
	Series           string
	SeriesIndex      float64
	Year             int
	Released         time.Time
	Overview         string
	Genres           []string
	Folder           string
	Artwork          []domain.Artwork
	Versions         []Version
}

// Fill takes what o says of a book that b leaves unsaid, as another version of it says it.
func (b *Book) Fill(o Book) {
	b.Author, b.Series, b.Overview = cmp.Or(b.Author, o.Author), cmp.Or(b.Series, o.Series), cmp.Or(b.Overview, o.Overview)
	b.SeriesIndex, b.Year = cmp.Or(b.SeriesIndex, o.SeriesIndex), cmp.Or(b.Year, o.Year)
	if b.Released.IsZero() {
		b.Released = o.Released
	}
	if len(b.Genres) == 0 {
		b.Genres = o.Genres
	}
	b.Artwork = append(b.Artwork, o.Artwork...)
}

// SaveBooksFolder writes a folder of a books library's books, each under its author, and remembers
// its fingerprint, nil for none, in one transaction. A book is found by its title under its author,
// so its formats in other folders join it.
func (s *Store) SaveBooksFolder(ctx context.Context, lib uuid.UUID, folder string, fingerprint []byte, books []Book) (Saved, error) {
	saved := Saved{Titles: Changed{}}
	err := s.pipelined(ctx, func(tx db) error {
		settings, err := analysisOf(ctx, tx, lib)
		if err != nil {
			return err
		}
		for _, b := range books {
			if err := saveBook(ctx, tx, lib, settings, folder, b, saved.Titles); err != nil {
				return fmt.Errorf("%s: %w", b.Title, err)
			}
		}
		return rememberFolder(ctx, tx, lib, folder, fingerprint)
	})
	return saved, err
}

func saveBook(ctx context.Context, tx db, lib uuid.UUID, settings analysis, folder string, b Book, changed Changed) error {
	authorFolder := "."
	if b.Folder != "." {
		authorFolder = path.Dir(b.Folder)
	}
	author, err := ensureNamed(ctx, tx, model.Item{LibraryID: &lib, Kind: domain.ItemAuthor, Folder: authorFolder},
		domain.Metadata{Title: b.Author}, changed)
	if err != nil {
		return err
	}
	book, err := ensureNamed(ctx, tx, model.Item{LibraryID: &lib, Kind: domain.ItemBook, ParentID: &author, Folder: b.Folder},
		domain.Metadata{
			Title: b.Title, SortTitle: b.SortTitle, Year: b.Year, ReleaseDate: b.Released, Overview: b.Overview, Genres: b.Genres,
		}, changed)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE items SET series = $2, series_index = $3 WHERE id = $1`, book, optional(b.Series), optional(b.SeriesIndex))
	if err != nil {
		return err
	}
	if err := saveFolderArtwork(ctx, tx, book, folder, b.Artwork); err != nil {
		return err
	}
	for _, v := range b.Versions {
		if err := saveVersion(ctx, tx, lib, settings, book, v); err != nil {
			return err
		}
	}
	return keyTitle(ctx, tx, author)
}

// BookPage is what only an author's or a book's page has: a book's author, and the series it is in
// and its place there; an author's books.
type BookPage struct {
	Author      *TitleRef
	Series      string
	SeriesIndex *float64
	Books       []Card
}

// bookOrder orders an author's books, of alias, by series and their place in it, as a shelf keeps
// them, and a book in no series by when it came out.
func bookOrder(alias string) string {
	return alias + ".series NULLS LAST, " + alias + ".series_index NULLS LAST, " + alias + ".released_asc, " + alias + ".sort_title, " + alias + ".id"
}

// Books answers an author's books as cards, in their order.
func (s *Store) Books(ctx context.Context, profile, author uuid.UUID) ([]Card, error) {
	rows, err := queryRows[model.Item](ctx, s.pool, `
		SELECT `+itemColumns+` FROM items WHERE parent_id = $1 AND kind = 'book' ORDER BY `+bookOrder("items"), author)
	if err != nil {
		return nil, err
	}
	return s.cards(ctx, profile, rows)
}

// ErrNotRead is a title that is no book, which no reader reads.
var ErrNotRead = errors.New("only a book is read")

// SaveReading keeps where the profile is in a book, as its reader says it, and how far through the
// book that is, at a time or now for none, wherever the book is listed, and answers how far that
// got: a reader at its end has read it, counted as a play. ErrSuperseded if the state has changed
// since, ErrNotRead for any other title, ErrNotFound for none.
func (s *Store) SaveReading(ctx context.Context, profile, book uuid.UUID, locator jsontext.Value, progression float64, at *time.Time) (domain.Reach, error) {
	reach := domain.ReadOf(progression)
	var kind domain.ItemKind
	if err := s.pool.QueryRow(ctx, `SELECT kind FROM items WHERE id = $1`, book).Scan(&kind); err != nil {
		return reach, found(err)
	}
	if kind != domain.ItemBook {
		return reach, ErrNotRead
	}
	var tag pgconn.CommandTag
	var err error
	if reach == domain.ReachEnd {
		tag, err = s.played(ctx, profile, book, at)
	} else {
		// A reader opened at the cover has read nothing: it moves nothing up Continue Reading.
		tag, err = s.pool.Exec(ctx, `
			INSERT INTO watch_state (profile_id, item_id, locator, progression, last_played_at, changed_at)
			SELECT $1, t, $3::jsonb, $4::real, CASE WHEN $4::real > 0 THEN w END, w
			FROM same_title($2) t, least($5::timestamptz, now()) w ORDER BY t
			ON CONFLICT (profile_id, item_id) DO UPDATE SET locator = excluded.locator, progression = excluded.progression,
				last_played_at = coalesce(excluded.last_played_at, watch_state.last_played_at),
				changed_at = excluded.changed_at`+newest, profile, book, locator, progression, at)
	}
	switch {
	case violates(err, foreignKeyViolation):
		return reach, ErrNotFound
	case err == nil && tag.RowsAffected() == 0:
		return reach, ErrSuperseded
	}
	return reach, err
}
