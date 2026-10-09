package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
)

var (
	ErrPluginExists = errors.New("a plugin with that id is already registered")
	// ErrUnknownPlugin is a library given a plugin's source that no plugin registered has.
	ErrUnknownPlugin = errors.New("no plugin registered has that id")
)

// Plugin is a plugin an admin registered: what it speaks, where it answers, and the manifest it
// last answered there, as it answered it.
type Plugin struct {
	Slug     string
	Protocol domain.PluginProtocol
	URL      string
	Manifest []byte
}

// Plugins answers every registered plugin, by slug.
func (s *Store) Plugins(ctx context.Context) ([]Plugin, error) {
	return queryStructs[Plugin](ctx, s.pool, `SELECT slug, protocol, url, manifest FROM plugins ORDER BY slug`)
}

// Plugin answers a registered plugin, or ErrNotFound.
func (s *Store) Plugin(ctx context.Context, slug string) (Plugin, error) {
	var p Plugin
	err := s.pool.QueryRow(ctx, `SELECT slug, protocol, url, manifest FROM plugins WHERE slug = $1`, slug).
		Scan(&p.Slug, &p.Protocol, &p.URL, &p.Manifest)
	return p, found(err)
}

// AddPlugin registers a plugin, or answers ErrPluginExists for a slug in use.
func (s *Store) AddPlugin(ctx context.Context, plugin Plugin) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO plugins (slug, protocol, url, manifest) VALUES ($1, $2, $3, $4)`,
		plugin.Slug, plugin.Protocol, plugin.URL, plugin.Manifest)
	if violates(err, uniqueViolation) {
		return ErrPluginExists
	}
	return err
}

// SetPluginManifest keeps the manifest a plugin answers now.
func (s *Store) SetPluginManifest(ctx context.Context, slug string, manifest []byte) error {
	return affected(s.pool.Exec(ctx, `UPDATE plugins SET manifest = $2 WHERE slug = $1`, slug, manifest))
}

// RemovePlugin forgets a plugin, its settings, and its place in each library's sources. What it
// said about titles stands as it is, ranked below everything, so the next source to speak of a
// field replaces it.
func (s *Store) RemovePlugin(ctx context.Context, slug string) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := affected(tx.Exec(ctx, `DELETE FROM plugins WHERE slug = $1`, slug)); err != nil {
			return err
		}
		source := domain.PluginSource(slug)
		if _, err := tx.Exec(ctx, `DELETE FROM library_sources WHERE source = $1`, source); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `DELETE FROM providers WHERE id = $1`, source)
		return err
	})
}

// registered refuses a plugin's source that no registered plugin has.
func registered(ctx context.Context, tx db, sources []*model.LibrarySource) error {
	for _, ls := range sources {
		slug, ok := ls.Source.Plugin()
		if !ok {
			continue
		}
		var known bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT FROM plugins WHERE slug = $1)`, slug).Scan(&known); err != nil {
			return err
		}
		if !known {
			return fmt.Errorf("%w: %s", ErrUnknownPlugin, ls.Source)
		}
	}
	return nil
}
