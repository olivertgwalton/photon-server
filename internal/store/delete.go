package store

import (
	"context"
	"errors"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// ErrDeletionOff refuses to delete a title whose library does not allow its titles' files deleted.
var ErrDeletionOff = errors.New("its library does not allow its titles' files to be deleted: allow it in the library's settings")

// LibraryFile is a file of a library: the library's root, and its path inside it as the scan
// recorded it.
type LibraryFile struct {
	Root string
	Rel  string
}

// TitleFiles answers every file a title is made of, so it may be deleted: each copy's parts and
// subtitles, a show's seasons' and episodes' and a film's extras' with them. A title whose library
// does not allow its titles' files deleted is refused, and a collection, made of no files of its
// own, is no title here.
func (s *Store) TitleFiles(ctx context.Context, id uuid.UUID) ([]LibraryFile, error) {
	item, err := readItem(ctx, s.pool, id)
	if err != nil {
		return nil, err
	}
	if item.Kind == domain.ItemCollection {
		return nil, ErrNotFound
	}
	var deletion domain.MediaDeletion
	if err := s.pool.QueryRow(ctx, `SELECT deletion FROM libraries WHERE id = $1`, item.LibraryID).Scan(&deletion); err != nil {
		return nil, found(err)
	}
	switch deletion {
	case domain.DeletionFiles:
	case domain.DeletionOff:
		return nil, ErrDeletionOff
	}
	return queryStructs[LibraryFile](ctx, s.pool, `
		WITH RECURSIVE under AS (
			SELECT id FROM items WHERE id = $1
			UNION ALL
			SELECT i.id FROM items i JOIN under u ON i.parent_id = u.id
		), copies AS (
			SELECT id FROM versions WHERE item_id IN (SELECT id FROM under)
		)
		SELECT l.root, f.rel_path AS rel FROM part_files f
		JOIN parts p ON p.id = f.part_id JOIN libraries l ON l.id = f.library_id
		WHERE p.version_id IN (SELECT id FROM copies)
		UNION
		SELECT l.root, f.rel_path FROM subtitle_files f JOIN libraries l ON l.id = f.library_id
		WHERE f.version_id IN (SELECT id FROM copies) AND f.body IS NULL
		ORDER BY rel`, id)
}

// ForgetTitle takes a title out of its library, its seasons, episodes and extras with it, once
// its files are deleted, and has its folder read again, so a season or show it emptied goes too.
func (s *Store) ForgetTitle(ctx context.Context, id uuid.UUID) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		item, err := readItem(ctx, tx, id)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM items WHERE id = $1`, id); err != nil {
			return err
		}
		return askScan(ctx, tx, item.LibraryID, []string{item.Folder}, 0)
	})
}
