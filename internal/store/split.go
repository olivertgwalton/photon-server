package store

import (
	"context"
	"errors"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
)

// ErrOneVersion refuses to split apart a film with one version.
var ErrOneVersion = errors.New("it has one version, so nothing to split apart")

// SplitTitle splits a film's versions apart, as Plex's Split Apart does: the version that plays by
// default stays, and each other becomes a film of its own, named as the files are and matched
// afresh. A scan leaves each where it was split to, as Plex's does not.
func (s *Store) SplitTitle(ctx context.Context, id uuid.UUID) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		item, err := readItem(ctx, tx, id)
		if err != nil {
			return err
		}
		if item.Kind != domain.ItemMovie {
			return ErrNotFound
		}
		versions, err := queryColumn[uuid.UUID](ctx, tx, `
			SELECT id FROM versions WHERE item_id = $1 ORDER BY `+versionOrder, id)
		if err != nil {
			return err
		}
		if len(versions) < 2 {
			return ErrOneVersion
		}
		for _, version := range versions[1:] {
			film := model.Item{
				LibraryID: item.LibraryID, Kind: domain.ItemMovie, ScanTitle: item.ScanTitle,
				Title: item.ScanTitle, SortTitle: sortTitle(item.ScanTitle), Folder: item.Folder,
			}
			if err := insertItem(ctx, tx, &film); err != nil {
				return err
			}
			if err := describe(ctx, tx, film.ID, film.ScanTitle, deref(item.Year), nil, nil); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE versions SET item_id = $2, split_at = now(), merged_at = NULL WHERE id = $1`, version, film.ID); err != nil {
				return err
			}
			if err := keyTitle(ctx, tx, film.ID); err != nil {
				return err
			}
			if err := enqueue(ctx, tx, domain.JobIdentify, film.ID); err != nil {
				return err
			}
		}
		return nil
	})
}
