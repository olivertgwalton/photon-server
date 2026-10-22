package store

import (
	"context"
	"errors"
	"regexp"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// ListRef is the list kept on a provider a list collection holds, as Kometa's list builders read
// one, and how many of its titles the libraries it reads lack.
type ListRef struct {
	Source  domain.FieldSource
	ID      string
	Missing int
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
