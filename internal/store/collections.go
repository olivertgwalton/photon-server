package store

import (
	"context"
	"errors"
	"regexp"
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

// shownCollections are the collections worth showing: an admin's, and a provider's once
// it holds minShown titles.
var shownCollections = `SELECT c.item_id FROM collections c
	WHERE c.origin IN ` + madeHere + ` OR (SELECT count(*) FROM collection_members m WHERE m.collection_id = c.item_id) >= ` + strconv.Itoa(minShown)

// listedCollection is whether items is a collection the viewer v finds listed, so the listing and
// the count of them cannot disagree.
var listedCollection = `items.kind = 'collection' AND items.id IN (` + shownCollections + `) AND sees(v, items)`

// holdsTitleOf is whether the collection items holds a title of the library lib, an SQL value: a
// collection, in no library, is listed in each library whose titles it holds, as Jellyfin lists a
// library's box sets.
func holdsTitleOf(lib string) string {
	return `EXISTS (SELECT 1 FROM collection_members held JOIN items title ON title.id = held.item_id
		WHERE held.collection_id = items.id AND title.library_id = ` + lib + `)`
}

// Collections answers a page of the collections holding a library's titles, by title, and how many
// there are.
func (s *Store) Collections(ctx context.Context, lib, profile uuid.UUID, offset, limit int) ([]Card, int64, error) {
	if err := hasLibrary(ctx, s.pool, lib); err != nil {
		return nil, 0, err
	}
	return s.collectionsWhere(ctx, profile, holdsTitleOf("@lib"), pgx.NamedArgs{"lib": lib}, offset, limit)
}

// AllCollections answers a page of every collection, by title, and how many there are: what
// Jellyfin's Collections view lists.
func (s *Store) AllCollections(ctx context.Context, profile uuid.UUID, offset, limit int) ([]Card, int64, error) {
	return s.collectionsWhere(ctx, profile, "true", pgx.NamedArgs{}, offset, limit)
}

// collectionsWhere answers a page of the listed collections cond holds of, by title, and how many
// there are.
func (s *Store) collectionsWhere(ctx context.Context, profile uuid.UUID, cond string, args pgx.NamedArgs, offset, limit int) ([]Card, int64, error) {
	args["profile"], args["offset"], args["limit"] = profile, offset, limit
	// The viewer is made once: joined to each collection, the certificates a limited profile may
	// not see were found again for every one, most of the time a library's list took.
	seer := `WITH seer AS MATERIALIZED (SELECT v FROM viewer(@profile) v) `
	where := ` FROM items, seer WHERE items.kind = 'collection' AND ` + cond + ` AND ` + listedCollection
	var rows []*model.Item
	total, err := s.counted(ctx, seer+`SELECT count(*)`+where, args, func(ctx context.Context) (err error) {
		rows, err = queryRows[model.Item](ctx, s.pool, seer+`SELECT `+itemColumnsOf("items")+where+` ORDER BY items.sort_title, items.id OFFSET @offset LIMIT @limit`, args)
		return err
	})
	if err != nil {
		return nil, 0, err
	}
	cards, err := s.cards(ctx, profile, rows)
	return cards, total, err
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

// AddSmartCollection makes an admin's collection whose titles a rule finds in libs.
func (s *Store) AddSmartCollection(ctx context.Context, libs []uuid.UUID, title string, rule SmartRule) (uuid.UUID, error) {
	if err := rule.CheckShared(); err != nil {
		return uuid.UUID{}, err
	}
	var id uuid.UUID
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := hasLibraries(ctx, tx, libs); err != nil {
			return err
		}
		var err error
		if id, err = newCollection(ctx, tx, title, madeBy{origin: domain.CollectionSmart, rule: &rule, libraries: libs}); err != nil {
			return err
		}
		if err := applyMetadata(ctx, tx, id, domain.SourceUser, domain.Metadata{Title: title}); err != nil {
			return err
		}
		return s.findMembers(ctx, tx, id, libs, rule)
	})
	return id, err
}

// SetRule replaces a smart collection's rule, and its titles with what it finds.
func (s *Store) SetRule(ctx context.Context, collection uuid.UUID, rule SmartRule) error {
	if err := rule.CheckShared(); err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		libs, err := userCollection(ctx, tx, collection, domain.CollectionSmart)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE collections SET rule = $2 WHERE item_id = $1`, collection, rule); err != nil {
			return err
		}
		return s.findMembers(ctx, tx, collection, libs, rule)
	})
}

// findMembers replaces a smart collection's titles with what its rule finds now, in its order.
func (s *Store) findMembers(ctx context.Context, tx db, collection uuid.UUID, libs []uuid.UUID, rule SmartRule) error {
	where, args, err := s.wallQuery(ctx, libs, uuid.UUID{}, domain.WallTitles, rule.Filter)
	if err != nil {
		return err
	}
	_, order := rule.page().orderBy(args)
	args["collection"] = collection
	limit := ""
	if rule.Limit > 0 {
		args["limit"], limit = rule.Limit, ` LIMIT @limit`
	}
	if _, err := tx.Exec(ctx, `DELETE FROM collection_members WHERE collection_id = @collection`, args); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO collection_members (collection_id, item_id, position)
		SELECT @collection, items.id, row_number() OVER (`+order+`) - 1 FROM items `+where+` `+order+limit, args)
	return err
}

// RefreshSmartCollections finds the titles of the smart collections reading any of libs again, or
// of every one where none is named.
func (s *Store) RefreshSmartCollections(ctx context.Context, libs ...uuid.UUID) error {
	type smart struct {
		ItemID    uuid.UUID
		Libraries []uuid.UUID
		Rule      SmartRule
	}
	rows, err := s.pool.Query(ctx, `
		SELECT item_id, libraries, rule FROM collections
		WHERE origin = 'smart' AND ($1::uuid[] IS NULL OR libraries && $1)`, libs)
	if err != nil {
		return err
	}
	all, err := pgx.CollectRows(rows, pgx.RowToStructByPos[smart])
	if err != nil {
		return err
	}
	for _, c := range all {
		if err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
			return s.findMembers(ctx, tx, c.ItemID, c.Libraries, c.Rule)
		}); err != nil {
			return err
		}
	}
	return nil
}

// ListRef is the list kept on a provider a list collection holds, as Kometa's list builders read
// one, and how many of its titles the libraries it reads lack.
type ListRef struct {
	Source  domain.FieldSource `json:"source"`
	ID      string             `json:"id"`
	Missing int                `json:"missing,omitzero"`
}

// ErrNoSuchList is a list id no provider could name.
var ErrNoSuchList = errors.New("a list is named by a provider's id for it, or as user/list")

// listID is what a list's id may be: a word, or two as "user/list", each starting with a letter or
// digit, so none is a path's "..".
var listID = regexp.MustCompile(`^\w[\w.-]*(/\w[\w.-]*)?$`)

// Check is whether a list is named as a provider names one.
func (l ListRef) Check() error {
	if !listID.MatchString(l.ID) {
		return ErrNoSuchList
	}
	return nil
}

// AddListCollection makes an admin's collection that holds a list's titles found in libs, as
// listed.
func (s *Store) AddListCollection(ctx context.Context, libs []uuid.UUID, title string, list ListRef, listed []domain.Listed) (uuid.UUID, error) {
	var id uuid.UUID
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := hasLibraries(ctx, tx, libs); err != nil {
			return err
		}
		var err error
		if id, err = newCollection(ctx, tx, title, madeBy{origin: domain.CollectionList, list: &list, libraries: libs}); err != nil {
			return err
		}
		if err := applyMetadata(ctx, tx, id, domain.SourceUser, domain.Metadata{Title: title}); err != nil {
			return err
		}
		return keepListed(ctx, tx, id, libs, listed)
	})
	return id, err
}

// SetListMembers replaces a list collection's titles with those its list holds of the libraries it
// reads, in its order, as Kometa's sync mode; it counts those they lack.
func (s *Store) SetListMembers(ctx context.Context, collection uuid.UUID, listed []domain.Listed) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		libs, err := userCollection(ctx, tx, collection, domain.CollectionList)
		if err != nil {
			return err
		}
		return keepListed(ctx, tx, collection, libs, listed)
	})
}

// keepListed puts in a collection the titles of libs a list holds, each found by its TMDB id or
// its IMDb id, once, where the list first has it.
func keepListed(ctx context.Context, tx db, collection uuid.UUID, libs []uuid.UUID, listed []domain.Listed) error {
	kinds, tmdb, imdb := make([]string, len(listed)), make([]string, len(listed)), make([]string, len(listed))
	for i, l := range listed {
		kinds[i], tmdb[i], imdb[i] = string(l.Kind), l.IDs[domain.ProviderTMDB], l.IDs[domain.ProviderIMDb]
	}
	if _, err := tx.Exec(ctx, `DELETE FROM collection_members WHERE collection_id = $1`, collection); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `
		WITH found AS (
			SELECT l.n, (
				SELECT e.item_id FROM external_ids e JOIN items i ON i.id = e.item_id AND i.library_id = ANY($2) AND i.kind = l.kind
				WHERE e.provider = 'tmdb' AND e.value = l.tmdb OR e.provider = 'imdb' AND e.value = l.imdb LIMIT 1
			) AS item_id
			FROM unnest($3::text[], $4::text[], $5::text[]) WITH ORDINALITY AS l(kind, tmdb, imdb, n)
		), kept AS (
			INSERT INTO collection_members (collection_id, item_id, position)
			SELECT $1, item_id, row_number() OVER (ORDER BY n) - 1 FROM (
				SELECT DISTINCT ON (item_id) item_id, n FROM found WHERE item_id IS NOT NULL ORDER BY item_id, n
			) first
		)
		UPDATE collections SET list_missing = (SELECT count(*) FROM found WHERE item_id IS NULL) WHERE item_id = $1`,
		collection, libs, kinds, tmdb, imdb)
	return err
}

// CollectionList answers the list a list collection holds; ErrNotFound for no collection, and
// ErrNotUserCollection for one made otherwise.
func (s *Store) CollectionList(ctx context.Context, collection uuid.UUID) (ListRef, error) {
	var l ListRef
	var origin domain.CollectionOrigin
	var source, id *string
	err := s.pool.QueryRow(ctx, `SELECT origin, list_source, list_id, list_missing FROM collections WHERE item_id = $1`, collection).
		Scan(&origin, &source, &id, &l.Missing)
	if err != nil {
		return l, found(err)
	}
	if origin != domain.CollectionList || source == nil || id == nil {
		return l, ErrNotUserCollection
	}
	l.Source, l.ID = domain.FieldSource(*source), *id
	return l, nil
}

// ListCollection is a list collection, and the list it holds.
type ListCollection struct {
	ID   uuid.UUID
	List ListRef
}

// ListCollections answers every list collection.
func (s *Store) ListCollections(ctx context.Context) ([]ListCollection, error) {
	rows, err := s.pool.Query(ctx, `SELECT item_id, list_source, list_id, list_missing FROM collections WHERE origin = 'list'`)
	if err != nil {
		return nil, err
	}
	var out []ListCollection
	var c ListCollection
	_, err = pgx.ForEachRow(rows, []any{&c.ID, &c.List.Source, &c.List.ID, &c.List.Missing}, func() error {
		out = append(out, c)
		return nil
	})
	return out, err
}
