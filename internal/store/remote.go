package store

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
)

// SaveListed makes a remote library hold the titles of its list, listed: one it lacks is added
// under the ids it is listed by, named as the list names it until it is matched; one the list no
// longer holds is removed, unless a profile has played, favourited or watchlisted it or anything in
// it. A title of a kind the library does not hold, or listed by no id, is passed over.
func (s *Store) SaveListed(ctx context.Context, lib uuid.UUID, kind domain.ItemKind, listed []domain.Listed) (Changed, error) {
	changed := Changed{}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		held := []uuid.UUID{}
		for _, l := range listed {
			if l.Kind != kind || len(l.IDs) == 0 {
				continue
			}
			id, err := listedItem(ctx, tx, lib, l)
			if err == nil {
				held = append(held, id)
				continue
			}
			if !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
			// A title the list does not name is called by an id it is listed by until it is matched.
			title := cmp.Or(l.Title, l.IDs[slices.Sorted(maps.Keys(l.IDs))[0]])
			item := model.Item{LibraryID: lib, Kind: kind, ScanTitle: title, Title: title, SortTitle: sortTitle(title)}
			if err := insertItem(ctx, tx, &item); err != nil {
				return err
			}
			if err := describe(ctx, tx, item.ID, title, l.Year, l.IDs, nil); err != nil {
				return err
			}
			if err := keyTitle(ctx, tx, item.ID); err != nil {
				return err
			}
			if err := enqueue(ctx, tx, domain.JobIdentify, item.ID); err != nil {
				return err
			}
			held = append(held, item.ID)
			changed.add(domain.TitleAdded, item.ID)
		}
		removed, err := queryColumn[uuid.UUID](ctx, tx, `
			DELETE FROM items i WHERE i.library_id = $1 AND i.kind = $2 AND NOT i.id = ANY($3)
				AND NOT EXISTS (SELECT 1 FROM items j LEFT JOIN items p ON p.id = j.parent_id
					WHERE j.library_id = $1 AND i.id IN (j.id, j.parent_id, p.parent_id)
						AND (EXISTS (SELECT 1 FROM watch_state w WHERE w.item_id = j.id)
							OR EXISTS (SELECT 1 FROM favourites f WHERE f.item_id = j.id)
							OR EXISTS (SELECT 1 FROM watchlist w WHERE w.item_id = j.id)))
			RETURNING i.id`, lib, kind, held)
		changed.add(domain.TitleRemoved, removed...)
		return err
	})
	return changed, err
}

// listedItem is the title of a remote library a list names by any of the ids it lists.
func listedItem(ctx context.Context, tx db, lib uuid.UUID, l domain.Listed) (uuid.UUID, error) {
	var providers, values []string
	for p, v := range l.IDs {
		providers, values = append(providers, string(p)), append(values, v)
	}
	var id uuid.UUID
	err := tx.QueryRow(ctx, `
		SELECT i.id FROM external_ids e JOIN items i ON i.id = e.item_id
		WHERE i.library_id = $1 AND i.kind = $2
			AND (e.provider, e.value) IN (SELECT * FROM unnest($3::text[], $4::text[]))
		LIMIT 1`, lib, l.Kind, providers, values).Scan(&id)
	return id, err
}

// mediaOf is where the media of an item's library is.
func mediaOf(ctx context.Context, tx db, item uuid.UUID) (domain.LibraryMedia, error) {
	var media domain.LibraryMedia
	err := tx.QueryRow(ctx, `SELECT l.media FROM items i JOIN libraries l ON l.id = i.library_id WHERE i.id = $1`, item).Scan(&media)
	return media, found(err)
}

// addAired adds to a remote show the season of a number and its episodes a provider says have
// aired, which a folder show has only from its files. One that has not aired, or whose day is not
// known, stays announced.
func addAired(ctx context.Context, tx db, show uuid.UUID, number int, episodes map[int]domain.Metadata) error {
	today := time.Now()
	var aired []int
	for n, e := range episodes {
		if !e.ReleaseDate.IsZero() && !e.ReleaseDate.After(today) {
			aired = append(aired, n)
		}
	}
	if len(aired) == 0 {
		return nil
	}
	var lib uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT library_id FROM items WHERE id = $1`, show).Scan(&lib); err != nil {
		return found(err)
	}
	season, err := ensureSeason(ctx, tx, lib, show, "", number, nil, Changed{})
	if err != nil {
		return err
	}
	have, err := queryColumn[int](ctx, tx, `
		SELECT episode_number FROM items WHERE parent_id = $1 AND kind = 'episode' AND episode_number IS NOT NULL`, season)
	if err != nil {
		return err
	}
	for _, n := range aired {
		if slices.Contains(have, n) {
			continue
		}
		e := episodes[n]
		title := cmp.Or(e.Title, fmt.Sprintf("Episode %d", n))
		row := model.Item{
			LibraryID: lib, Kind: domain.ItemEpisode, ParentID: &season, SeasonNumber: &number, EpisodeNumber: &n,
			AirDate: &e.ReleaseDate, ScanTitle: title, Title: title, SortTitle: sortTitle(title),
		}
		if err := insertItem(ctx, tx, &row); err != nil {
			return err
		}
	}
	return nil
}

// RemoteTitle is a film or an episode of a remote library, as its provider is asked for its
// streams.
type RemoteTitle struct {
	Library uuid.UUID
	Source  domain.FieldSource
	Title   domain.Streamed
}

// RemoteTitleOf answers what a remote library's film or episode is asked for by; false for
// anything else.
func (s *Store) RemoteTitleOf(ctx context.Context, item uuid.UUID) (RemoteTitle, bool, error) {
	var r RemoteTitle
	var kind domain.ItemKind
	var season, episode *int
	var show *uuid.UUID
	err := s.pool.QueryRow(ctx, `
		SELECT i.library_id, l.stream_source, i.kind, i.season_number, i.episode_number, se.parent_id
		FROM items i JOIN libraries l ON l.id = i.library_id LEFT JOIN items se ON se.id = i.parent_id
		WHERE i.id = $1 AND l.media = 'remote' AND i.kind IN ('movie', 'episode')`, item).
		Scan(&r.Library, &r.Source, &kind, &season, &episode, &show)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, false, nil
	}
	if err != nil {
		return r, false, err
	}
	r.Title, err = s.streamed(ctx, item, kind, season, episode, show)
	return r, err == nil, err
}

// streamed is a film, or an episode of a show in a season, as a provider is asked for its streams.
func (s *Store) streamed(ctx context.Context, item uuid.UUID, kind domain.ItemKind, season, episode *int, show *uuid.UUID) (domain.Streamed, error) {
	t := domain.Streamed{Kind: domain.ItemMovie}
	if kind == domain.ItemEpisode && show != nil {
		item = *show
		t = domain.Streamed{Kind: domain.ItemShow, Season: deref(season), Episode: deref(episode)}
	}
	ids, err := s.ExternalIDs(ctx, []uuid.UUID{item})
	t.IDs = ids[item]
	return t, err
}

// Offered marks missing a remote title's copies its provider no longer offers, and not missing
// those it offers again, the fingerprints of which are offered, as a scan marks a folder's copies
// by the files it finds. It answers the fingerprints of the title's copies that are offered.
func (s *Store) Offered(ctx context.Context, item uuid.UUID, offered [][]byte) ([][]byte, error) {
	return queryColumn[[]byte](ctx, s.pool, `
		WITH marked AS (
			UPDATE versions v SET missing_since = CASE WHEN v.fingerprint = ANY($2) THEN NULL ELSE coalesce(v.missing_since, now()) END
			WHERE v.item_id = $1 RETURNING v.fingerprint, v.missing_since IS NULL AS live)
		SELECT fingerprint FROM marked WHERE live`, item, offered)
}

// SaveCopy keeps a copy of a title no scan finds: a remote library's, read as it is played.
func (s *Store) SaveCopy(ctx context.Context, lib, item uuid.UUID, c Copy) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		settings, err := analysisOf(ctx, tx, lib)
		if err != nil {
			return err
		}
		if err := saveCopy(ctx, tx, lib, settings, item, c); err != nil {
			return err
		}
		return keyTitle(ctx, tx, item)
	})
}

// Streamed answers which of items are films or episodes of remote libraries, whose copies their
// providers stream.
func (s *Store) Streamed(ctx context.Context, items []uuid.UUID) (map[uuid.UUID]bool, error) {
	ids, err := queryColumn[uuid.UUID](ctx, s.pool, `
		SELECT i.id FROM items i JOIN libraries l ON l.id = i.library_id
		WHERE i.id = ANY($1) AND l.media = 'remote' AND i.kind IN ('movie', 'episode')`, items)
	out := make(map[uuid.UUID]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out, err
}
