package store

import (
	"context"
	"errors"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// saveFolderThemes replaces a title's theme files in folder with those found there now, in their
// order.
func saveFolderThemes(ctx context.Context, tx db, item uuid.UUID, folder string, files []string) error {
	_, err := tx.Exec(ctx, `DELETE FROM themes WHERE item_id = $1 AND source = $2 AND folder = $3`, item, domain.ThemeFromFile, folder)
	if err != nil || len(files) == 0 {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO themes (item_id, source, place, position, folder)
		SELECT $1, $2, place, position - 1, $3 FROM unnest($4::text[]) WITH ORDINALITY AS f(place, position)`,
		item, domain.ThemeFromFile, folder, files)
	return err
}

// askThemes queues a theme fetch for each of the films and shows items selects whose library takes
// ThemerrDB's, that TMDB or IMDb knows, and that have no theme file of their own. One already
// fetched is asked again, so a link ThemerrDB has changed since is fetched anew.
func askThemes(ctx context.Context, tx db, items string, args pgx.NamedArgs) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO jobs (kind, subject)
		SELECT 'theme', i.id FROM items i JOIN libraries l ON l.id = i.library_id
		WHERE i.id IN (`+items+`) AND i.kind IN ('movie', 'show') AND l.themes = 'themerr'
			AND EXISTS (SELECT 1 FROM external_ids e WHERE e.item_id = i.id AND e.provider IN ('tmdb', 'imdb'))
			AND NOT EXISTS (SELECT 1 FROM themes t WHERE t.item_id = i.id AND t.source = 'file')`+requeue, args)
	return err
}

// ThemeSubject is a film or show to look up in ThemerrDB, by its ids, and the theme last fetched for
// it, if any, and the YouTube link it was fetched from.
type ThemeSubject struct {
	Kind  domain.ItemKind
	TMDB  string
	IMDb  string
	Theme uuid.UUID
	URL   string
}

// ThemeSubject answers the film or show to fetch a theme for, and false for a title that has gone,
// has a theme file of its own, or whose library no longer takes ThemerrDB's.
func (s *Store) ThemeSubject(ctx context.Context, id uuid.UUID) (ThemeSubject, bool, error) {
	var t ThemeSubject
	var theme *uuid.UUID
	err := s.pool.QueryRow(ctx, `
		SELECT i.kind, coalesce(tmdb.value, ''), coalesce(imdb.value, ''), t.id,
			coalesce(t.place, '') FROM items i
		JOIN libraries l ON l.id = i.library_id
		LEFT JOIN external_ids tmdb ON tmdb.item_id = i.id AND tmdb.provider = 'tmdb'
		LEFT JOIN external_ids imdb ON imdb.item_id = i.id AND imdb.provider = 'imdb'
		LEFT JOIN themes t ON t.item_id = i.id AND t.source = 'themerr'
		WHERE i.id = $1 AND i.kind IN ('movie', 'show') AND l.themes = 'themerr'
			AND NOT EXISTS (SELECT 1 FROM themes f WHERE f.item_id = i.id AND f.source = 'file')`,
		id).Scan(&t.Kind, &t.TMDB, &t.IMDb, &theme, &t.URL)
	if errors.Is(err, pgx.ErrNoRows) {
		return t, false, nil
	}
	t.Theme = deref(theme)
	return t, err == nil, err
}

// SaveFetchedTheme records a title's theme fetched from the YouTube link url, kept under id, in
// place of one fetched before. A title gone meanwhile is let be.
func (s *Store) SaveFetchedTheme(ctx context.Context, item, id uuid.UUID, url string) error {
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM themes WHERE item_id = $1 AND source = $2`, item, domain.ThemeFromThemerr); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO themes (id, item_id, source, place, position) VALUES ($1, $2, $3, $4, 0)`,
			id, item, domain.ThemeFromThemerr, url)
		return err
	})
	if violates(err, foreignKeyViolation) {
		return nil
	}
	return err
}

// ThemeFile is where a theme tune is: a file under Root, or, fetched from ThemerrDB's link, in the
// server's cache under the theme's id.
type ThemeFile struct {
	Source domain.ThemeSource
	Root   string
	Path   string
}

// Theme answers where a theme tune is, or ErrNotFound.
func (s *Store) Theme(ctx context.Context, id uuid.UUID) (ThemeFile, error) {
	var f ThemeFile
	var place string
	err := s.pool.QueryRow(ctx, `
		SELECT t.source, t.place, l.root FROM themes t
		JOIN items i ON i.id = t.item_id JOIN libraries l ON l.id = i.library_id
		WHERE t.id = $1`, id).Scan(&f.Source, &place, &f.Root)
	if errors.Is(err, pgx.ErrNoRows) {
		return f, ErrNotFound
	}
	switch f.Source {
	case domain.ThemeFromFile:
		f.Path = place
	case domain.ThemeFromThemerr:
		f.Root = ""
	}
	return f, err
}

// themes answers the theme tunes played under a film's or show's page, as its library offers
// them: its files, else ThemerrDB's.
func (s *Store) themes(ctx context.Context, item uuid.UUID) ([]uuid.UUID, error) {
	return queryColumn[uuid.UUID](ctx, s.pool, `
		SELECT t.id FROM themes t
		JOIN items i ON i.id = t.item_id JOIN libraries l ON l.id = i.library_id
		WHERE t.item_id = $1
			AND (l.themes = 'themerr' OR (l.themes = 'local' AND t.source = 'file'))
			AND (t.source = 'file' OR NOT EXISTS (SELECT 1 FROM themes f WHERE f.item_id = t.item_id AND f.source = 'file'))
		ORDER BY t.position`, item)
}
