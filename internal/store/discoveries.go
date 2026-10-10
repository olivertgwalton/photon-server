package store

import (
	"context"
	"crypto/sha256"
	"errors"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
)

// discoveriesFor is how long a title a search found is kept for a profile to open.
const discoveriesFor = 24 * time.Hour

// Discovery is a title a provider's search found for a remote library that holds no title by its
// id: shown as one of the library's, and made one as it is opened.
type Discovery struct {
	ID       uuid.UUID
	Library  uuid.UUID
	Kind     domain.ItemKind
	Title    string
	Year     int
	Overview string
	Poster   uuid.UUID
}

// Card is a discovery as a wall or a search shows a title.
func (d Discovery) Card() Card {
	return Card{ID: d.ID, Kind: d.Kind, Title: d.Title, Year: d.Year, Poster: d.Poster, Overview: d.Overview}
}

// Discoverable is a remote library a profile may see whose titles a provider's search finds.
type Discoverable struct {
	ID     uuid.UUID
	Kind   domain.LibraryKind
	Source domain.FieldSource
}

// Discoverable answers the remote libraries a profile may see that search a provider: none for a
// profile held to an age, as a title found has no certificate until it is described.
func (s *Store) Discoverable(ctx context.Context, profile uuid.UUID) ([]Discoverable, error) {
	return queryStructs[Discoverable](ctx, s.pool, `
		SELECT l.id, l.kind, l.discover_source AS source FROM libraries l, viewer($1) v
		WHERE l.media = 'remote' AND l.discover_source IS NOT NULL AND v.max_age IS NULL
			AND (v.libraries IS NULL OR l.id = ANY (v.libraries))
		ORDER BY l.name`, profile)
}

// SaveDiscoveries keeps what a provider's search found for a remote library, under fixed ids, and
// answers those no library holds a title of by the provider's id. What was found a day ago goes.
func (s *Store) SaveDiscoveries(ctx context.Context, lib uuid.UUID, kind domain.ItemKind, provider domain.Provider, found []domain.Candidate) ([]Discovery, error) {
	var out []Discovery
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM discoveries WHERE found_at < now() - $1::interval`, discoveriesFor); err != nil {
			return err
		}
		for _, c := range found {
			var held bool
			err := tx.QueryRow(ctx, `
				SELECT EXISTS (SELECT 1 FROM external_ids e JOIN items i ON i.id = e.item_id
					WHERE e.provider = $1 AND e.value = $2 AND i.kind = $3)`, provider, c.ID, kind).Scan(&held)
			if err != nil {
				return err
			}
			if held {
				continue
			}
			d := Discovery{
				ID: derived("title", lib, provider, c.ID), Library: lib, Kind: kind, Title: c.Title, Year: c.Year,
				Overview: c.Overview, Poster: derived("poster", lib, provider, c.ID),
			}
			_, err = tx.Exec(ctx, `
				INSERT INTO discoveries (id, library_id, kind, provider, value, title, year, overview, poster_id, poster_url)
				VALUES ($1, $2, $3, $4, $5, $6, nullif($7, 0), nullif($8, ''), $9, nullif($10, ''))
				ON CONFLICT (id) DO UPDATE SET title = excluded.title, year = excluded.year, overview = excluded.overview,
					poster_url = excluded.poster_url, found_at = now()`,
				d.ID, lib, kind, provider, c.ID, d.Title, d.Year, d.Overview, d.Poster, c.Poster)
			if err != nil {
				return err
			}
			if c.Poster == "" {
				d.Poster = uuid.UUID{}
			}
			out = append(out, d)
		}
		return nil
	})
	return out, err
}

// derived is a fixed id of what a provider's id names in a library, as RFC 9562's version 8 lays
// one out: the same each time it is found, so a client that kept it finds it again.
func derived(what string, lib uuid.UUID, provider domain.Provider, id string) uuid.UUID {
	sum := sha256.Sum256([]byte(what + "\x00" + lib.String() + "\x00" + string(provider) + "\x00" + id))
	var u uuid.UUID
	copy(u[:], sum[:16])
	u[6] = u[6]&0x0f | 0x80
	u[8] = u[8]&0x3f | 0x80
	return u
}

// HoldDiscovered makes a discovery a title of its library, under the discovery's id, named as it
// was found and matched by the id it was found under. False for an id that is no discovery, or one
// already held.
func (s *Store) HoldDiscovered(ctx context.Context, id uuid.UUID) (bool, error) {
	var held bool
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var d struct {
			Library  uuid.UUID
			Kind     domain.ItemKind
			Provider domain.Provider
			Value    string
			Title    string
			Year     *int
		}
		err := tx.QueryRow(ctx, `
			SELECT library_id, kind, provider, value, title, year FROM discoveries d
			WHERE id = $1 AND NOT EXISTS (SELECT 1 FROM items WHERE id = d.id)
			FOR UPDATE`, id).Scan(&d.Library, &d.Kind, &d.Provider, &d.Value, &d.Title, &d.Year)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		row := model.Item{ID: id, LibraryID: d.Library, Kind: d.Kind, ScanTitle: d.Title, Title: d.Title, SortTitle: sortTitle(d.Title)}
		_, err = tx.Exec(ctx, `
			INSERT INTO items (id, library_id, kind, scan_title, title, sort_title, folder) VALUES ($1, $2, $3, $4, $5, $6, '')`,
			row.ID, row.LibraryID, row.Kind, row.ScanTitle, row.Title, row.SortTitle)
		if err != nil {
			return err
		}
		if err := describe(ctx, tx, id, d.Title, deref(d.Year), map[domain.Provider]string{d.Provider: d.Value}, nil); err != nil {
			return err
		}
		if err := keyTitle(ctx, tx, id); err != nil {
			return err
		}
		held = true
		return enqueue(ctx, tx, domain.JobIdentify, id)
	})
	return held, err
}
