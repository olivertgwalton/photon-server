package store

import (
	"context"
	"errors"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
)

// Owner names the title an extra belongs to.
type Owner struct {
	Kind domain.ItemKind
	// Folder is a film's own folder, or the series folder of a show, season or episode.
	Folder string
	// Title tells apart films that share a folder.
	Title   string
	Season  int
	Episode int
}

type Extra struct {
	Kind   domain.ExtraKind
	Title  string
	Folder string
	Owner  Owner
	Copy   Copy
}

var errNoOwner = errors.New("no single title owns it")

// saveExtras saves each extra under its owner, answering the paths of those whose owner the
// catalogue does not hold, or holds twice, so the scanner can say so rather than guess.
func saveExtras(ctx context.Context, tx db, lib uuid.UUID, settings analysis, extras []Extra) ([]string, error) {
	var unowned []string
	for _, e := range extras {
		owner, err := ownerOf(ctx, tx, lib, e.Owner)
		if errors.Is(err, errNoOwner) {
			unowned = append(unowned, e.Copy.Parts[0].RelPath)
			continue
		}
		if err != nil {
			return nil, err
		}
		if err := saveExtra(ctx, tx, lib, settings, owner, e); err != nil {
			return nil, err
		}
	}
	return unowned, nil
}

func ownerOf(ctx context.Context, tx db, lib uuid.UUID, o Owner) (uuid.UUID, error) {
	switch o.Kind {
	case domain.ItemMovie:
		sql := `SELECT id FROM items WHERE library_id = $1 AND kind = 'movie' AND folder = $2`
		args := []any{lib, o.Folder}
		if o.Title != "" {
			sql += ` AND title = $3`
			args = append(args, o.Title)
		}
		return one(ctx, tx, sql, args...)
	case domain.ItemCollection:
		return uuid.UUID{}, ErrNotFound
	case domain.ItemShow, domain.ItemSeason, domain.ItemEpisode:
		show, err := one(ctx, tx, `SELECT id FROM items WHERE library_id = $1 AND kind = 'show' AND folder = $2`, lib, o.Folder)
		if err != nil || o.Kind == domain.ItemShow {
			return show, err
		}
		season, err := one(ctx, tx, `SELECT id FROM items WHERE parent_id = $1 AND kind = 'season' AND season_number = $2`, show, o.Season)
		if err != nil || o.Kind == domain.ItemSeason {
			return season, err
		}
		return one(ctx, tx, `SELECT id FROM items WHERE parent_id = $1 AND kind = 'episode' AND episode_number = $2`, season, o.Episode)
	case domain.ItemExtra:
	}
	return uuid.UUID{}, errNoOwner
}

// one is the single item a query finds; none or several is no owner.
func one(ctx context.Context, tx db, sql string, args ...any) (uuid.UUID, error) {
	rows, err := tx.Query(ctx, sql+` LIMIT 2`, args...)
	if err != nil {
		return uuid.UUID{}, err
	}
	found, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return uuid.UUID{}, err
	}
	if len(found) != 1 {
		return uuid.UUID{}, errNoOwner
	}
	return found[0], nil
}

func saveExtra(ctx context.Context, tx db, lib uuid.UUID, settings analysis, owner uuid.UUID, e Extra) error {
	row := model.Item{
		LibraryID: lib, Kind: domain.ItemExtra, ParentID: &owner, ExtraKind: &e.Kind,
		ScanTitle: e.Title, Title: e.Title, SortTitle: sortTitle(e.Title), Folder: e.Folder,
	}
	var version uuid.UUID
	var kind domain.ItemKind
	err := tx.QueryRow(ctx, `
		SELECT v.id, i.id, i.kind FROM versions v JOIN items i ON i.id = v.item_id
		WHERE v.library_id = $1 AND v.fingerprint = $2 LIMIT 1`, lib, e.Copy.ContentKey).Scan(&version, &row.ID, &kind)
	switch {
	case err == nil:
		// The same bytes are already a film's or an episode's copy: this file is another place to
		// read it, and that title stays what it is.
		if kind != domain.ItemExtra {
			return addPlaces(ctx, tx, lib, version, e.Copy)
		}
		_, err = tx.Exec(ctx, `UPDATE items SET parent_id = $2, extra_kind = $3, scan_title = $4, folder = $5 WHERE id = $1`,
			row.ID, row.ParentID, row.ExtraKind, row.ScanTitle, row.Folder)
	case errors.Is(err, pgx.ErrNoRows):
		err = insertItem(ctx, tx, &row)
	}
	if err != nil {
		return err
	}
	if err := describe(ctx, tx, row.ID, e.Title, 0, nil, nil); err != nil {
		return err
	}
	return saveCopy(ctx, tx, lib, settings, row.ID, e.Copy)
}
