package store

import (
	"cmp"
	"context"
	"errors"
	"maps"
	"slices"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
)

// SaveListed makes a remote library hold the titles of its list, listed: one it lacks is added
// under the ids it is listed by, named as the list names it until it is matched; one the list no
// longer holds is removed, unless a profile has played, favourited or watchlisted it or anything in
// it. A title of a kind the library does not hold, or listed by no id, is passed over.
func (s *Store) SaveListed(ctx context.Context, lib uuid.UUID, kind domain.ItemKind, listed []domain.Listed) (Changed, error) {
	changed := Changed{}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		held := []uuid.UUID{}
		for _, l := range listed {
			if l.Kind != kind || len(l.IDs) == 0 {
				continue
			}
			id, err := listedItem(ctx, tx, lib, l)
			if err == nil {
				held = append(held, id)
				continue
			}
			if !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
			// A title the list does not name is called by an id it is listed by until it is matched.
			title := cmp.Or(l.Title, l.IDs[slices.Sorted(maps.Keys(l.IDs))[0]])
			item := model.Item{LibraryID: lib, Kind: kind, ScanTitle: title, Title: title, SortTitle: sortTitle(title)}
			if err := insertItem(ctx, tx, &item); err != nil {
				return err
			}
			if err := describe(ctx, tx, item.ID, title, l.Year, l.IDs, nil); err != nil {
				return err
			}
			if err := keyTitle(ctx, tx, item.ID); err != nil {
				return err
			}
			if err := enqueue(ctx, tx, domain.JobIdentify, item.ID); err != nil {
				return err
			}
			held = append(held, item.ID)
			changed.add(domain.TitleAdded, item.ID)
		}
		removed, err := queryColumn[uuid.UUID](ctx, tx, `
			DELETE FROM items i WHERE i.library_id = $1 AND i.kind = $2 AND NOT i.id = ANY($3)
				AND NOT EXISTS (SELECT 1 FROM items j LEFT JOIN items p ON p.id = j.parent_id
					WHERE j.library_id = $1 AND i.id IN (j.id, j.parent_id, p.parent_id)
						AND (EXISTS (SELECT 1 FROM watch_state w WHERE w.item_id = j.id)
							OR EXISTS (SELECT 1 FROM favourites f WHERE f.item_id = j.id)
							OR EXISTS (SELECT 1 FROM watchlist w WHERE w.item_id = j.id)))
			RETURNING i.id`, lib, kind, held)
		changed.add(domain.TitleRemoved, removed...)
		return err
	})
	return changed, err
}

// listedItem is the title of a remote library a list names by any of the ids it lists.
func listedItem(ctx context.Context, tx db, lib uuid.UUID, l domain.Listed) (uuid.UUID, error) {
	var providers, values []string
	for p, v := range l.IDs {
		providers, values = append(providers, string(p)), append(values, v)
	}
	var id uuid.UUID
	err := tx.QueryRow(ctx, `
		SELECT i.id FROM external_ids e JOIN items i ON i.id = e.item_id
		WHERE i.library_id = $1 AND i.kind = $2
			AND (e.provider, e.value) IN (SELECT * FROM unnest($3::text[], $4::text[]))
		LIMIT 1`, lib, l.Kind, providers, values).Scan(&id)
	return id, err
}
