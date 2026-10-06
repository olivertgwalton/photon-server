package store

import (
	"context"
	"errors"
	"strconv"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
)

// ErrNotUserCollection is a collection a provider made, which only that provider changes.
var ErrNotUserCollection = errors.New("only a collection an admin made is changed by hand")

// minShown is how many titles a provider's collection must hold before it is shown, as Plex's
// minimum automatic collection size: a box set of one is no set.
const minShown = 2

// groupingProviders are the providers whose box sets become collections, by the id they file them
// under.
var groupingProviders = map[domain.FieldSource]domain.Provider{domain.SourceTMDB: domain.ProviderTMDB}

// saveGroupings puts a title in the box sets a source names it part of, making each the first
// time, and takes it out of that source's others.
func saveGroupings(ctx context.Context, tx db, item uuid.UUID, source domain.FieldSource, groupings []domain.Grouping) error {
	by, ok := groupingProviders[source]
	if !ok {
		return nil
	}
	var lib uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT library_id FROM items WHERE id = $1`, item).Scan(&lib); err != nil {
		return found(err)
	}
	keep := []uuid.UUID{}
	for _, g := range groupings {
		var id uuid.UUID
		err := tx.QueryRow(ctx, `
			SELECT c.item_id FROM collections c
			JOIN items i ON i.id = c.item_id AND i.library_id = $1
			JOIN external_ids e ON e.item_id = c.item_id AND e.provider = $2 AND e.value = $3
			WHERE c.origin = $4 LIMIT 1`, lib, by, g.ID, source).Scan(&id)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			if id, err = newCollection(ctx, tx, lib, g.Title, domain.CollectionOrigin(source)); err != nil {
				return err
			}
			if err := saveIDs(ctx, tx, id, domain.IDFromMatch, map[domain.Provider]string{by: g.ID}); err != nil {
				return err
			}
			if err := keyTitle(ctx, tx, id); err != nil {
				return err
			}
		case err != nil:
			return err
		}
		if err := applyMetadata(ctx, tx, id, source, domain.Metadata{Title: g.Title}); err != nil {
			return err
		}
		if err := saveProviderArtwork(ctx, tx, id, source, g.Artwork); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO collection_members (collection_id, item_id, position) VALUES ($1, $2, 0)
			ON CONFLICT (collection_id, item_id) DO UPDATE SET position = excluded.position`, id, item)
		if err != nil {
			return err
		}
		keep = append(keep, id)
	}
	// Out of the source's sets it no longer names; one left empty goes at the next scan.
	_, err := tx.Exec(ctx, `
		DELETE FROM collection_members m USING collections c
		WHERE m.collection_id = c.item_id AND c.origin = $1 AND m.item_id = $2 AND m.collection_id <> ALL($3)`,
		source, item, keep)
	return err
}

// newCollection makes a collection in a library, made by origin.
func newCollection(ctx context.Context, tx db, lib uuid.UUID, title string, origin domain.CollectionOrigin) (uuid.UUID, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx, `
		INSERT INTO items (library_id, kind, title, scan_title, sort_title, folder) VALUES ($1, 'collection', $2, $2, $3, '')
		RETURNING id`, lib, title, sortTitle(title)).Scan(&id)
	if err != nil {
		return id, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO collections (item_id, origin) VALUES ($1, $2)`, id, origin)
	return id, err
}

// shownCollections are a library's collections worth showing: an admin's, and a provider's once
// it holds minShown titles.
var shownCollections = `SELECT c.item_id FROM collections c
	WHERE c.origin = 'user' OR (SELECT count(*) FROM collection_members m WHERE m.collection_id = c.item_id) >= ` + strconv.Itoa(minShown)

// listedCollection is whether items is a collection the viewer v finds listed in its library, so
// the listing and the library's count of them cannot disagree.
var listedCollection = `items.kind = 'collection' AND items.id IN (` + shownCollections + `) AND sees(v, items)`

// Collections answers a page of a library's collections, by title, and how many there are.
func (s *Store) Collections(ctx context.Context, lib, profile uuid.UUID, offset, limit int) ([]Card, int64, error) {
	if err := hasLibrary(ctx, s.pool, lib); err != nil {
		return nil, 0, err
	}
	where := ` FROM items WHERE items.library_id = $1 AND EXISTS (SELECT 1 FROM viewer($2) v WHERE ` + listedCollection + `)`
	var total int64
	if err := s.pool.QueryRow(ctx, `SELECT count(*)`+where, lib, profile).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := queryRows[model.Item](ctx, s.pool, `SELECT `+itemColumns+where+` ORDER BY items.sort_title, items.id OFFSET $3 LIMIT $4`,
		lib, profile, offset, limit)
	if err != nil {
		return nil, 0, err
	}
	cards, err := s.cards(ctx, profile, rows)
	return cards, total, err
}

// memberOrder is a collection's titles' order, with its row as c and theirs as m and items: an
// admin's in the order they were put, a provider's from the first released.
const memberOrder = `CASE WHEN c.origin = 'user' THEN m.position END, items.released_asc, items.sort_title, items.id`

// Members answers a collection's titles, in memberOrder. ErrNotFound for no such collection.
func (s *Store) Members(ctx context.Context, profile, collection uuid.UUID) ([]Card, error) {
	if err := s.pool.QueryRow(ctx, `SELECT 1 FROM collections WHERE item_id = $1`, collection).Scan(new(int)); err != nil {
		return nil, found(err)
	}
	rows, err := queryRows[model.Item](ctx, s.pool, `
		SELECT `+itemColumns+` FROM items
		JOIN collection_members m ON m.item_id = items.id AND m.collection_id = $1
		JOIN collections c ON c.item_id = m.collection_id
		WHERE EXISTS (SELECT 1 FROM viewer($2) v WHERE sees(v, items)) ORDER BY `+memberOrder, collection, profile)
	if err != nil {
		return nil, err
	}
	return s.cards(ctx, profile, rows)
}

// origins answers who made each collection among rows.
func (s *Store) origins(ctx context.Context, rows []*model.Item) (map[uuid.UUID]domain.CollectionOrigin, error) {
	out := map[uuid.UUID]domain.CollectionOrigin{}
	var in []uuid.UUID
	for _, r := range rows {
		if r.Kind == domain.ItemCollection {
			in = append(in, r.ID)
		}
	}
	if len(in) == 0 {
		return out, nil
	}
	var id uuid.UUID
	var origin domain.CollectionOrigin
	found, err := s.pool.Query(ctx, `SELECT item_id, origin FROM collections WHERE item_id = ANY($1)`, in)
	if err != nil {
		return out, err
	}
	_, err = pgx.ForEachRow(found, []any{&id, &origin}, func() error {
		out[id] = origin
		return nil
	})
	return out, err
}

// collectionsOf answers the shown collections a title is in, by title.
func (s *Store) collectionsOf(ctx context.Context, item uuid.UUID) ([]CollectionCard, error) {
	rows, err := queryRows[model.Item](ctx, s.pool, `
		SELECT `+itemColumns+` FROM items
		WHERE items.id IN (SELECT collection_id FROM collection_members WHERE item_id = $1) AND items.id IN (`+shownCollections+`)
		ORDER BY items.sort_title`, item)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	pictures, hashes, err := s.pictureOrder(ctx, rows)
	if err != nil {
		return nil, err
	}
	out := make([]CollectionCard, len(rows))
	for n, r := range rows {
		poster := first(pictures[r.ID][domain.ArtworkPoster])
		out[n] = CollectionCard{ID: r.ID, Title: r.Title, Poster: poster, Blurhashes: blurhashesOf(hashes, poster)}
	}
	return out, nil
}

// AddCollection makes an admin's collection in a library.
func (s *Store) AddCollection(ctx context.Context, lib uuid.UUID, title string) (uuid.UUID, error) {
	var id uuid.UUID
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := hasLibrary(ctx, tx, lib); err != nil {
			return err
		}
		var err error
		if id, err = newCollection(ctx, tx, lib, title, domain.CollectionUser); err != nil {
			return err
		}
		return applyMetadata(ctx, tx, id, domain.SourceUser, domain.Metadata{Title: title})
	})
	return id, err
}

// SetMembers replaces an admin's collection's titles with these, in this order. Each must be a
// film or show of the collection's library.
func (s *Store) SetMembers(ctx context.Context, collection uuid.UUID, items []uuid.UUID) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		lib, err := userCollection(ctx, tx, collection)
		if err != nil {
			return err
		}
		if hasRepeats(items) {
			return ErrNotFound
		}
		var titles int
		err = tx.QueryRow(ctx, `
			SELECT count(*) FROM items WHERE library_id = $1 AND kind IN ('movie', 'show') AND id = ANY($2)`,
			lib, items).Scan(&titles)
		if err != nil {
			return err
		}
		if titles != len(items) {
			return ErrNotFound
		}
		if _, err := tx.Exec(ctx, `DELETE FROM collection_members WHERE collection_id = $1`, collection); err != nil {
			return err
		}
		if len(items) == 0 {
			return nil
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO collection_members (collection_id, item_id, position)
			SELECT $1, id, position - 1 FROM unnest($2::uuid[]) WITH ORDINALITY AS m(id, position)`, collection, items)
		return err
	})
}

// RemoveCollection removes an admin's collection; its titles are left as they are.
func (s *Store) RemoveCollection(ctx context.Context, collection uuid.UUID) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := userCollection(ctx, tx, collection); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `DELETE FROM items WHERE id = $1`, collection)
		return err
	})
}

// userCollection answers the library of an admin's collection; ErrNotFound for no collection, and
// ErrNotUserCollection for a provider's.
func userCollection(ctx context.Context, tx db, collection uuid.UUID) (uuid.UUID, error) {
	var origin domain.CollectionOrigin
	var lib uuid.UUID
	err := tx.QueryRow(ctx, `
		SELECT c.origin, i.library_id FROM collections c JOIN items i ON i.id = c.item_id WHERE c.item_id = $1`,
		collection).Scan(&origin, &lib)
	if err != nil {
		return uuid.UUID{}, found(err)
	}
	if origin != domain.CollectionUser {
		return uuid.UUID{}, ErrNotUserCollection
	}
	return lib, nil
}

func hasRepeats(ids []uuid.UUID) bool {
	seen := map[uuid.UUID]bool{}
	for _, id := range ids {
		if seen[id] {
			return true
		}
		seen[id] = true
	}
	return false
}
