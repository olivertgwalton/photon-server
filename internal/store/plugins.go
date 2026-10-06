package store

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
	"github.com/olivertgwalton/photon-server/internal/store/query"
)

var (
	ErrPluginExists = errors.New("a plugin with that id is already registered")
	// ErrUnknownPlugin is a library given a plugin's source that no plugin registered has.
	ErrUnknownPlugin = errors.New("no plugin registered has that id")
)

// Plugin is a metadata plugin an admin registered: where it answers, and the manifest it last
// answered there, as the plugin package encoded it.
type Plugin struct {
	Slug     string
	URL      string
	Manifest []byte
}

// Plugins answers every registered plugin, by slug.
func (s *Store) Plugins(ctx context.Context) ([]Plugin, error) {
	p := s.q.Plugin
	rows, err := p.WithContext(ctx).Order(p.Slug).Find()
	if err != nil {
		return nil, err
	}
	out := make([]Plugin, len(rows))
	for n, r := range rows {
		out[n] = Plugin{Slug: r.Slug, URL: r.URL, Manifest: r.Manifest}
	}
	return out, nil
}

// Plugin answers a registered plugin, or ErrNotFound.
func (s *Store) Plugin(ctx context.Context, slug string) (Plugin, error) {
	p := s.q.Plugin
	row, err := p.WithContext(ctx).Where(p.Slug.Eq(slug)).Take()
	if err != nil {
		return Plugin{}, found(err)
	}
	return Plugin{Slug: row.Slug, URL: row.URL, Manifest: row.Manifest}, nil
}

// AddPlugin registers a plugin, or answers ErrPluginExists for a slug in use.
func (s *Store) AddPlugin(ctx context.Context, plugin Plugin) error {
	err := s.q.Plugin.WithContext(ctx).Create(&model.Plugin{Slug: plugin.Slug, URL: plugin.URL, Manifest: plugin.Manifest})
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return ErrPluginExists
	}
	return err
}

// SetPluginManifest keeps the manifest a plugin answers now.
func (s *Store) SetPluginManifest(ctx context.Context, slug string, manifest []byte) error {
	p := s.q.Plugin
	res, err := p.WithContext(ctx).Where(p.Slug.Eq(slug)).Update(p.Manifest, manifest)
	if err == nil && res.RowsAffected == 0 {
		err = ErrNotFound
	}
	return err
}

// RemovePlugin forgets a plugin, its settings, and its place in each library's sources. What it
// said about titles stands as it is, ranked below everything, so the next source to speak of a
// field replaces it.
func (s *Store) RemovePlugin(ctx context.Context, slug string) error {
	return s.q.Transaction(func(tx *query.Query) error {
		p, ls, pr := tx.Plugin, tx.LibrarySource, tx.Provider
		res, err := p.WithContext(ctx).Where(p.Slug.Eq(slug)).Delete()
		if err != nil {
			return err
		}
		if res.RowsAffected == 0 {
			return ErrNotFound
		}
		source := string(domain.PluginSource(slug))
		if _, err := ls.WithContext(ctx).Where(ls.Source.Eq(source)).Delete(); err != nil {
			return err
		}
		_, err = pr.WithContext(ctx).Where(pr.ID.Eq(source)).Delete()
		return err
	})
}

// registered refuses a plugin's source that no registered plugin has.
func registered(ctx context.Context, tx *query.Query, sources []*model.LibrarySource) error {
	p := tx.Plugin
	for _, ls := range sources {
		slug, ok := ls.Source.Plugin()
		if !ok {
			continue
		}
		n, err := p.WithContext(ctx).Where(p.Slug.Eq(slug)).Count()
		if err != nil {
			return err
		}
		if n == 0 {
			return fmt.Errorf("%w: %s", ErrUnknownPlugin, ls.Source)
		}
	}
	return nil
}
