package store

import (
	"context"
	"errors"
	"uuid"

	"github.com/jackc/pgx/v5"
	"gorm.io/gorm"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
	"github.com/olivertgwalton/photon-server/internal/store/query"
)

// saveFolderThemes replaces a title's theme files in folder with those found there now, in their
// order.
func saveFolderThemes(ctx context.Context, tx *query.Query, item model.UUID, folder string, files []string) error {
	t := tx.Theme
	if _, err := t.WithContext(ctx).Where(t.ItemID.Eq(item), t.Source.Eq(string(domain.ThemeFromFile)), t.Folder.Eq(folder)).Delete(); err != nil {
		return err
	}
	if len(files) == 0 {
		return nil
	}
	rows := make([]*model.Theme, len(files))
	for n, f := range files {
		rows[n] = &model.Theme{ItemID: item, Source: domain.ThemeFromFile, Place: f, Position: int16(n), Folder: &folder}
	}
	return t.WithContext(ctx).Create(rows...)
}

// askThemes queues a theme fetch for each of the shows items selects whose library takes the theme
// host, that TheTVDB knows and that have no theme yet.
func askThemes(ctx context.Context, tx *query.Query, items string, args map[string]any) error {
	return tx.Job.WithContext(ctx).UnderlyingDB().Exec(`
		INSERT INTO jobs (kind, subject)
		SELECT 'theme', i.id FROM items i JOIN libraries l ON l.id = i.library_id
		WHERE i.id IN (`+items+`) AND i.kind = 'show' AND l.themes = 'all'
			AND EXISTS (SELECT 1 FROM external_ids e WHERE e.item_id = i.id AND e.provider = 'tvdb')
			AND NOT EXISTS (SELECT 1 FROM themes t WHERE t.item_id = i.id)`+requeue, args).Error
}

// ThemeSubject answers the TheTVDB id of a show to fetch a theme for, and false for a title that
// has gone, has a theme, or whose library no longer takes the theme host.
func (s *Store) ThemeSubject(ctx context.Context, id uuid.UUID) (string, bool, error) {
	var tvdb string
	err := s.pool.QueryRow(ctx, `
		SELECT e.value FROM items i
		JOIN libraries l ON l.id = i.library_id
		JOIN external_ids e ON e.item_id = i.id AND e.provider = 'tvdb'
		WHERE i.id = $1 AND l.themes = 'all' AND NOT EXISTS (SELECT 1 FROM themes t WHERE t.item_id = i.id)`,
		id.String()).Scan(&tvdb)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	return tvdb, err == nil, err
}

// SaveFetchedTheme records a show's theme from the theme host at url, kept under id. A show gone
// meanwhile is let be.
func (s *Store) SaveFetchedTheme(ctx context.Context, show, id uuid.UUID, url string) error {
	err := s.q.Theme.WithContext(ctx).Create(&model.Theme{
		ID: model.UUID(id), ItemID: model.UUID(show), Source: domain.ThemeFromTVThemes, Place: url,
	})
	if errors.Is(err, gorm.ErrForeignKeyViolated) || errors.Is(err, gorm.ErrDuplicatedKey) {
		return nil
	}
	return err
}

// ThemeFile is where a theme tune is: a file under Root, or the theme host's URL.
type ThemeFile struct {
	Root string
	Path string
	URL  string
}

// Theme answers where a theme tune is, or ErrNotFound.
func (s *Store) Theme(ctx context.Context, id uuid.UUID) (ThemeFile, error) {
	var f ThemeFile
	var source domain.ThemeSource
	var place string
	err := s.pool.QueryRow(ctx, `
		SELECT t.source, t.place, l.root FROM themes t
		JOIN items i ON i.id = t.item_id JOIN libraries l ON l.id = i.library_id
		WHERE t.id = $1`, id.String()).Scan(&source, &place, &f.Root)
	if errors.Is(err, pgx.ErrNoRows) {
		return f, ErrNotFound
	}
	switch source {
	case domain.ThemeFromFile:
		f.Path = place
	case domain.ThemeFromTVThemes:
		f.Root, f.URL = "", place
	}
	return f, err
}

// themes answers the theme tunes played under a film's or show's page, as its library offers
// them: its files, else the theme host's.
func (s *Store) themes(ctx context.Context, item uuid.UUID) ([]uuid.UUID, error) {
	return queryIDs(ctx, s.pool, `
		SELECT t.id::text FROM themes t
		JOIN items i ON i.id = t.item_id JOIN libraries l ON l.id = i.library_id
		WHERE t.item_id = $1
			AND (l.themes = 'all' OR (l.themes = 'local' AND t.source = 'file'))
			AND (t.source = 'file' OR NOT EXISTS (SELECT 1 FROM themes f WHERE f.item_id = t.item_id AND f.source = 'file'))
		ORDER BY t.position`, item.String())
}
