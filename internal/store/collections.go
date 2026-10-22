package store

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
)

// ErrNotUserCollection is a collection not made as it is asked to be changed: one a provider made,
// which only it changes, or a smart collection's titles, which are its rule's.
var ErrNotUserCollection = errors.New("a collection is changed as it was made: an admin's by hand, a smart one by its rule")

// madeHere are the origins of the collections an admin made, shown however few titles they hold,
// in the order they were made in.
const madeHere = `('user', 'smart', 'list')`

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
	keep := []uuid.UUID{}
	for _, g := range groupings {
		// One box set of a source's, whichever libraries its titles are in. Titles are matched
		// at once, so the first to find none makes it while the others wait to find it.
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, string(source)+"/"+g.ID); err != nil {
			return err
		}
		var id uuid.UUID
		err := tx.QueryRow(ctx, `
			SELECT c.item_id FROM collections c
			JOIN external_ids e ON e.item_id = c.item_id AND e.provider = $1 AND e.value = $2
			WHERE c.origin = $3 LIMIT 1`, by, g.ID, source).Scan(&id)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			if id, err = newCollection(ctx, tx, g.Title, madeBy{origin: domain.CollectionOrigin(source)}); err != nil {
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

// madeBy is how a collection is made: by whom, by the rule or list its titles come from, and the
// libraries those are read in.
type madeBy struct {
	origin    domain.CollectionOrigin
	rule      *SmartRule
	list      *ListRef
	libraries []uuid.UUID
}

// newCollection makes a collection, in no library, made as by says.
func newCollection(ctx context.Context, tx db, title string, by madeBy) (uuid.UUID, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx, `
		INSERT INTO items (kind, title, scan_title, sort_title, folder) VALUES ('collection', $1, $1, $2, '')
		RETURNING id`, title, sortTitle(title)).Scan(&id)
	if err != nil {
		return id, err
	}
	var source, list *string
	if by.list != nil {
		source, list = (*string)(&by.list.Source), &by.list.ID
	}
	_, err = tx.Exec(ctx, `INSERT INTO collections (item_id, origin, rule, list_source, list_id, libraries) VALUES ($1, $2, $3, $4, $5, $6)`,
		id, by.origin, by.rule, source, list, by.libraries)
	return id, err
}

// hasLibraries is whether every one of libs is a library, and there is one; ErrNotFound if not.
func hasLibraries(ctx context.Context, q db, libs []uuid.UUID) error {
	var n int
	if err := q.QueryRow(ctx, `SELECT count(*) FROM libraries WHERE id = ANY($1)`, libs).Scan(&n); err != nil {
		return err
	}
	if n == 0 || n != len(libs) || hasRepeats(libs) {
		return ErrNotFound
	}
	return nil
}

// shownCollections are those worth showing: an admin's, and a provider's holding minShown titles.
var shownCollections = `SELECT c.item_id FROM collections c
	WHERE c.origin IN ` + madeHere + ` OR (SELECT count(*) FROM collection_members m WHERE m.collection_id = c.item_id) >= ` + strconv.Itoa(minShown)

// listedCollection is whether items is a collection the viewer v finds listed, so the listing and
// the count of them cannot disagree.
var listedCollection = `items.kind = 'collection' AND items.id IN (` + shownCollections + `) AND sees(v, items)`

// listedFrom is how many of a library's titles the collection c holds to be listed there, as its
// wall: a collection, in no library, is listed in each library holding as many, as Jellyfin's are.
var listedFrom = `CASE WHEN c.origin IN ` + madeHere + ` THEN 1 ELSE ` + strconv.Itoa(minShown) + ` END`

// holdsTitleOf is whether the collection items holds listedFrom titles of the library lib.
func holdsTitleOf(lib string) string {
	return `(SELECT count(*) FROM (SELECT FROM collection_members held JOIN items title ON title.id = held.item_id
			WHERE held.collection_id = items.id AND title.library_id = ` + lib + ` LIMIT ` + strconv.Itoa(minShown) + `) held)
		>= (SELECT ` + listedFrom + ` FROM collections c WHERE c.item_id = items.id)`
}

// Collections answers a page of the collections holding a library's titles, by title, and what the
// page tells of the rest.
func (s *Store) Collections(ctx context.Context, lib, profile uuid.UUID, page Page) ([]Card, Extent, error) {
	if err := hasLibrary(ctx, s.pool, lib); err != nil {
		return nil, Extent{}, err
	}
	return s.collectionsWhere(ctx, profile, holdsTitleOf("@lib"), pgx.NamedArgs{"lib": lib}, page)
}

// AllCollections answers a page of every collection, by title, and what the page tells of the rest:
// what Jellyfin's Collections view lists.
func (s *Store) AllCollections(ctx context.Context, profile uuid.UUID, page Page) ([]Card, Extent, error) {
	return s.collectionsWhere(ctx, profile, "true", pgx.NamedArgs{}, page)
}

// collectionsWhere is Collections and AllCollections of the listed collections cond holds of.
func (s *Store) collectionsWhere(ctx context.Context, profile uuid.UUID, cond string, args pgx.NamedArgs, page Page) ([]Card, Extent, error) {
	args["profile"], args["offset"], args["limit"] = profile, page.Offset, page.Limit+1
	// The viewer is made once: joined to each collection, the certificates a limited profile may
	// not see were found again for every one, most of the time a library's list took.
	seer := `WITH seer AS MATERIALIZED (SELECT v FROM viewer(@profile) v) `
	where := ` FROM items, seer WHERE items.kind = 'collection' AND ` + cond + ` AND ` + listedCollection
	var rows []*model.Item
	total, err := s.counted(ctx, page.Count, seer+`SELECT count(*)`+where, args, func(ctx context.Context) (err error) {
		rows, err = queryRows[model.Item](ctx, s.pool, seer+`SELECT `+itemColumnsOf("items")+where+` ORDER BY items.sort_title, items.id OFFSET @offset LIMIT @limit`, args)
		return err
	})
	if err != nil {
		return nil, Extent{}, err
	}
	rows, more := pageOf(rows, page.Limit)
	cards, err := s.cards(ctx, profile, rows)
	return cards, Extent{More: more, Total: total}, err
}

// HasCollections reports whether a profile finds any collection listed.
func (s *Store) HasCollections(ctx context.Context, profile uuid.UUID) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM items, viewer($1) v WHERE `+listedCollection+`)`, profile).Scan(&ok)
	return ok, err
}

// memberOrder is a collection's titles' order, with its row as c and theirs as m and items: an
// admin's in the order they were put or its rule found them, a provider's from the first released.
const memberOrder = `CASE WHEN c.origin IN ` + madeHere + ` THEN m.position END, items.released_asc, items.sort_title, items.id`

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

// boxSet is who made a collection, and when its first title came out, which it is dated by as
// Jellyfin dates a box set: the earliest of its titles' release dates, else of their years.
type boxSet struct {
	ItemID   uuid.UUID
	Origin   domain.CollectionOrigin
	Released *time.Time
	Year     *int
}

// boxSets answers each collection among rows as a boxSet.
func (s *Store) boxSets(ctx context.Context, rows []*model.Item) (map[uuid.UUID]boxSet, error) {
	var in []uuid.UUID
	for _, r := range rows {
		if r.Kind == domain.ItemCollection {
			in = append(in, r.ID)
		}
	}
	if len(in) == 0 {
		return nil, nil
	}
	sets, err := queryStructs[boxSet](ctx, s.pool, `
		SELECT c.item_id, c.origin, min(t.release_date) AS released,
			coalesce(extract(year FROM min(t.release_date))::int, min(t.year)) AS year
		FROM collections c LEFT JOIN collection_members m ON m.collection_id = c.item_id LEFT JOIN items t ON t.id = m.item_id
		WHERE c.item_id = ANY($1) GROUP BY c.item_id`, in)
	out := make(map[uuid.UUID]boxSet, len(sets))
	for _, set := range sets {
		out[set.ItemID] = set
	}
	return out, err
}

// firstReleased is when the collection items' first title came out, as a key of the wall sorted
// in dir: undated, it goes last whichever way, as released_asc and released_desc pad a title.
func firstReleased(dir string) string {
	pad := "'9999-12-31'"
	if dir == "DESC" {
		pad = "'0001-01-01'"
	}
	return `(SELECT coalesce(min(t.release_date), make_date(min(t.year), 1, 1), ` + pad + `)
		FROM collection_members m JOIN items t ON t.id = m.item_id WHERE m.collection_id = items.id)`
}

// collectionsOf answers the shown collections a title is in, or its season or show is, by title,
// as profile is shown them: a season or show in one has it named on each of its episodes' pages.
func (s *Store) collectionsOf(ctx context.Context, profile, item uuid.UUID) ([]CollectionCard, error) {
	rows, err := queryRows[model.Item](ctx, s.pool, `
		SELECT `+itemColumns+` FROM items
		WHERE items.id IN (SELECT collection_id FROM collection_members WHERE item_id IN (
				SELECT $1::uuid UNION SELECT parent_id FROM items WHERE id = $1
				UNION SELECT p.parent_id FROM items i JOIN items p ON p.id = i.parent_id WHERE i.id = $1))
			AND items.id IN (`+shownCollections+`)
		ORDER BY items.sort_title`, item)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	pictures, hashes, err := s.bestPictures(ctx, rows)
	if err != nil {
		return nil, err
	}
	posters, err := s.collectionPosters(ctx, profile, rows)
	if err != nil {
		return nil, err
	}
	wearPosters(pictures, hashes, posters)
	out := make([]CollectionCard, len(rows))
	for n, r := range rows {
		poster := first(pictures[r.ID][domain.ArtworkPoster])
		out[n] = CollectionCard{ID: r.ID, Title: r.Title, Poster: poster, Blurhashes: blurhashesOf(hashes, poster)}
	}
	return out, nil
}

// AddCollection makes an admin's collection.
func (s *Store) AddCollection(ctx context.Context, title string) (uuid.UUID, error) {
	var id uuid.UUID
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		if id, err = newCollection(ctx, tx, title, madeBy{origin: domain.CollectionUser}); err != nil {
			return err
		}
		return applyMetadata(ctx, tx, id, domain.SourceUser, domain.Metadata{Title: title})
	})
	return id, err
}

// memberKinds are what an admin's collection holds, of any library: a film, or a show, a season
// or an episode, as Jellyfin's box sets do; nothing an extra's or a collection.
const memberKinds = `kind NOT IN ('extra', 'collection')`

// SetMembers replaces an admin's collection's titles with these, in this order. Each must be one
// of memberKinds.
func (s *Store) SetMembers(ctx context.Context, collection uuid.UUID, items []uuid.UUID) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := userCollection(ctx, tx, collection, domain.CollectionUser); err != nil {
			return err
		}
		if hasRepeats(items) {
			return ErrNotFound
		}
		var titles int
		err := tx.QueryRow(ctx, `SELECT count(*) FROM items WHERE `+memberKinds+` AND id = ANY($1)`, items).Scan(&titles)
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

// AddMembers puts titles, each one of memberKinds, last in an admin's collection in this order; one
// already in it stays where it is.
func (s *Store) AddMembers(ctx context.Context, collection uuid.UUID, items []uuid.UUID) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := userCollection(ctx, tx, collection, domain.CollectionUser); err != nil {
			return err
		}
		var titles int
		err := tx.QueryRow(ctx, `SELECT count(*) FROM items WHERE `+memberKinds+` AND id = ANY($1)`, items).Scan(&titles)
		if err != nil {
			return err
		}
		if hasRepeats(items) || titles != len(items) {
			return ErrNotFound
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO collection_members (collection_id, item_id, position)
			SELECT $1, id, (SELECT coalesce(max(position) + 1, 0) FROM collection_members WHERE collection_id = $1) + n - 1
			FROM unnest($2::uuid[]) WITH ORDINALITY AS m(id, n)
			ON CONFLICT DO NOTHING`, collection, items)
		return err
	})
}

// RemoveCollection removes an admin's collection, made by hand, by a rule or by a list; its titles
// are left as they are.
func (s *Store) RemoveCollection(ctx context.Context, collection uuid.UUID) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := userCollection(ctx, tx, collection, domain.CollectionUser, domain.CollectionSmart, domain.CollectionList); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `DELETE FROM items WHERE id = $1`, collection)
		return err
	})
}

// SetBoxSet sets how a library's wall shows a collection, whoever made it; ErrNotFound for no
// collection.
func (s *Store) SetBoxSet(ctx context.Context, collection uuid.UUID, mode domain.CollectionBoxSet) error {
	tag, err := s.pool.Exec(ctx, `UPDATE collections SET box_set = $2 WHERE item_id = $1`, collection, mode)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// userCollection answers the libraries a collection made as one of made reads, where it is a
// smart or list collection; ErrNotFound for no collection, and ErrNotUserCollection for one made
// otherwise.
func userCollection(ctx context.Context, tx db, collection uuid.UUID, made ...domain.CollectionOrigin) ([]uuid.UUID, error) {
	var origin domain.CollectionOrigin
	var libs []uuid.UUID
	err := tx.QueryRow(ctx, `SELECT origin, libraries FROM collections WHERE item_id = $1`, collection).Scan(&origin, &libs)
	if err != nil {
		return nil, found(err)
	}
	if !slices.Contains(made, origin) {
		return nil, ErrNotUserCollection
	}
	return libs, nil
}
