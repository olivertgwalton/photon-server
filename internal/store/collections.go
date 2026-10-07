package store

import (
	"cmp"
	"context"
	"errors"
	"regexp"
	"slices"
	"strconv"
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
			if id, err = newCollection(ctx, tx, lib, g.Title, madeBy{origin: domain.CollectionOrigin(source)}); err != nil {
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

// madeBy is how a collection is made: by whom, and by the rule or list its titles come from.
type madeBy struct {
	origin domain.CollectionOrigin
	rule   *SmartRule
	list   *ListRef
}

// newCollection makes a collection in a library, made as by says.
func newCollection(ctx context.Context, tx db, lib uuid.UUID, title string, by madeBy) (uuid.UUID, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx, `
		INSERT INTO items (library_id, kind, title, scan_title, sort_title, folder) VALUES ($1, 'collection', $2, $2, $3, '')
		RETURNING id`, lib, title, sortTitle(title)).Scan(&id)
	if err != nil {
		return id, err
	}
	var source, list *string
	if by.list != nil {
		source, list = (*string)(&by.list.Source), &by.list.ID
	}
	_, err = tx.Exec(ctx, `INSERT INTO collections (item_id, origin, rule, list_source, list_id) VALUES ($1, $2, $3, $4, $5)`,
		id, by.origin, by.rule, source, list)
	return id, err
}

// shownCollections are a library's collections worth showing: an admin's, and a provider's once
// it holds minShown titles.
var shownCollections = `SELECT c.item_id FROM collections c
	WHERE c.origin IN ` + madeHere + ` OR (SELECT count(*) FROM collection_members m WHERE m.collection_id = c.item_id) >= ` + strconv.Itoa(minShown)

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
		if id, err = newCollection(ctx, tx, lib, title, madeBy{origin: domain.CollectionUser}); err != nil {
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
		lib, err := userCollection(ctx, tx, collection, domain.CollectionUser)
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

// SetPlacement sets where a collection is shown, an admin's or a provider's.
func (s *Store) SetPlacement(ctx context.Context, collection uuid.UUID, placement domain.CollectionPlacement) error {
	tag, err := s.pool.Exec(ctx, `UPDATE collections SET placement = $2 WHERE item_id = $1`, collection, placement)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
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

// userCollection answers the library of a collection made as one of made; ErrNotFound for no
// collection, and ErrNotUserCollection for one made otherwise.
func userCollection(ctx context.Context, tx db, collection uuid.UUID, made ...domain.CollectionOrigin) (uuid.UUID, error) {
	var origin domain.CollectionOrigin
	var lib uuid.UUID
	err := tx.QueryRow(ctx, `
		SELECT c.origin, i.library_id FROM collections c JOIN items i ON i.id = c.item_id WHERE c.item_id = $1`,
		collection).Scan(&origin, &lib)
	if err != nil {
		return uuid.UUID{}, found(err)
	}
	if !slices.Contains(made, origin) {
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

// SmartRule is what a smart collection's titles are, as Plex's smart collections: its library's
// films or shows as a wall's filter narrows them, in a wall's order, at most Limit of them, 0 for
// all. Its titles are the same for everyone, so it reads no profile's marks or plays, and each
// viewer sees what they may of them.
type SmartRule struct {
	Filter WallFilter      `json:"filter"`
	Sort   domain.WallSort `json:"sort,omitzero"`
	Order  domain.Order    `json:"order,omitzero"`
	Limit  int             `json:"limit,omitzero"`
}

// ErrRuleForSomeone is a rule that would read one profile's marks or plays.
var ErrRuleForSomeone = errors.New("a smart collection is everyone's: its rule reads no one's marks or plays")

// Check is whether a rule finds the same titles for everyone, with values a wall takes.
func (r SmartRule) Check() error {
	if len(r.Filter.Marks) > 0 || r.Sort == domain.SortPlayed {
		return ErrRuleForSomeone
	}
	if r.Limit < 0 {
		return errors.New("limit is 0, for all, or more")
	}
	return r.Filter.Check()
}

// page is the wall a rule reads, in title order where it names none.
func (r SmartRule) page() WallPage {
	sort := cmp.Or(r.Sort, domain.SortTitle)
	return WallPage{
		Sort: sort, Order: cmp.Or(r.Order, sort.DefaultOrder()),
		RatingSite: cmp.Or(r.Filter.RatingSite, domain.SiteIMDb), Filter: r.Filter,
	}
}

// AddSmartCollection makes an admin's collection in a library whose titles a rule finds.
func (s *Store) AddSmartCollection(ctx context.Context, lib uuid.UUID, title string, rule SmartRule) (uuid.UUID, error) {
	if err := rule.Check(); err != nil {
		return uuid.UUID{}, err
	}
	var id uuid.UUID
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := hasLibrary(ctx, tx, lib); err != nil {
			return err
		}
		var err error
		if id, err = newCollection(ctx, tx, lib, title, madeBy{origin: domain.CollectionSmart, rule: &rule}); err != nil {
			return err
		}
		if err := applyMetadata(ctx, tx, id, domain.SourceUser, domain.Metadata{Title: title}); err != nil {
			return err
		}
		return s.findMembers(ctx, tx, id, lib, rule)
	})
	return id, err
}

// SetRule replaces a smart collection's rule, and its titles with what it finds.
func (s *Store) SetRule(ctx context.Context, collection uuid.UUID, rule SmartRule) error {
	if err := rule.Check(); err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		lib, err := userCollection(ctx, tx, collection, domain.CollectionSmart)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE collections SET rule = $2 WHERE item_id = $1`, collection, rule); err != nil {
			return err
		}
		return s.findMembers(ctx, tx, collection, lib, rule)
	})
}

// findMembers replaces a smart collection's titles with what its rule finds now, in its order.
func (s *Store) findMembers(ctx context.Context, tx db, collection, lib uuid.UUID, rule SmartRule) error {
	titles, args, err := s.wallQuery(ctx, []uuid.UUID{lib}, uuid.UUID{}, rule.Filter)
	if err != nil {
		return err
	}
	order := rule.page().orderBy(args)
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
		SELECT @collection, items.id, row_number() OVER (`+order+`) - 1 `+titles+` AND items.kind <> 'collection' `+order+limit, args)
	return err
}

// RefreshSmartCollections finds the titles of the smart collections of libs again, or of every
// library's where none is named.
func (s *Store) RefreshSmartCollections(ctx context.Context, libs ...uuid.UUID) error {
	type smart struct {
		ItemID, LibraryID uuid.UUID
		Rule              SmartRule
	}
	rows, err := s.pool.Query(ctx, `
		SELECT c.item_id, i.library_id, c.rule FROM collections c JOIN items i ON i.id = c.item_id
		WHERE c.origin = 'smart' AND ($1::uuid[] IS NULL OR i.library_id = ANY($1))`, libs)
	if err != nil {
		return err
	}
	all, err := pgx.CollectRows(rows, pgx.RowToStructByPos[smart])
	if err != nil {
		return err
	}
	for _, c := range all {
		if err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
			return s.findMembers(ctx, tx, c.ItemID, c.LibraryID, c.Rule)
		}); err != nil {
			return err
		}
	}
	return nil
}

// ListRef is the list kept on a provider a list collection holds, as Kometa's list builders read
// one, and how many of its titles its library lacks.
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

// AddListCollection makes an admin's collection in a library that holds a list's titles, as
// listed.
func (s *Store) AddListCollection(ctx context.Context, lib uuid.UUID, title string, list ListRef, listed []domain.Listed) (uuid.UUID, error) {
	var id uuid.UUID
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := hasLibrary(ctx, tx, lib); err != nil {
			return err
		}
		var err error
		if id, err = newCollection(ctx, tx, lib, title, madeBy{origin: domain.CollectionList, list: &list}); err != nil {
			return err
		}
		if err := applyMetadata(ctx, tx, id, domain.SourceUser, domain.Metadata{Title: title}); err != nil {
			return err
		}
		return keepListed(ctx, tx, id, lib, listed)
	})
	return id, err
}

// SetListMembers replaces a list collection's titles with those of its library its list holds, in
// its order, as Kometa's sync mode; it counts those the library lacks.
func (s *Store) SetListMembers(ctx context.Context, collection uuid.UUID, listed []domain.Listed) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		lib, err := userCollection(ctx, tx, collection, domain.CollectionList)
		if err != nil {
			return err
		}
		return keepListed(ctx, tx, collection, lib, listed)
	})
}

// keepListed puts in a collection the titles of its library a list holds, each found by its TMDB
// id or its IMDb id, once, where the list first has it.
func keepListed(ctx context.Context, tx db, collection, lib uuid.UUID, listed []domain.Listed) error {
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
				SELECT e.item_id FROM external_ids e JOIN items i ON i.id = e.item_id AND i.library_id = $2 AND i.kind = l.kind
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
		collection, lib, kinds, tmdb, imdb)
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
