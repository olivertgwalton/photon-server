package store

import (
	"context"
	"errors"
	"slices"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// Subject is what a film or show is matched to a provider by, with the seasons of a show that
// a provider has yet to describe.
type Subject struct {
	Kind    domain.ItemKind
	Title   string
	Year    int
	IDs     map[domain.Provider]string
	Seasons []int
	// Order is the order a show's episode files are numbered in.
	Order domain.EpisodeOrder
	// Sources are what its library asks for metadata or pictures of any kind it holds.
	Sources []domain.FieldSource
	// Unmatched is an admin's unmatching it: no provider is asked about it until it is released,
	// though an admin may search them to fix its match.
	Unmatched bool
	// Locale is what its library asks in; what it leaves unsaid is the server's.
	Locale domain.Locale
}

// IdentifySubject answers what is known of a title to match it by, or false for one that has gone.
func (s *Store) IdentifySubject(ctx context.Context, id uuid.UUID) (Subject, bool, error) {
	item, err := readItem(ctx, s.pool, id)
	if errors.Is(err, ErrNotFound) {
		return Subject{}, false, nil
	}
	if err != nil {
		return Subject{}, false, err
	}
	unmatched, err := held(ctx, s.pool, id)
	if err != nil {
		return Subject{}, false, err
	}
	sub := Subject{Kind: item.Kind, Title: item.Title, IDs: map[domain.Provider]string{}, Order: item.EpisodeOrder, Unmatched: unmatched}
	if item.Year != nil {
		sub.Year = *item.Year
	}
	var language, country *string
	err = s.pool.QueryRow(ctx, `SELECT metadata_language, certification_country, artwork_language FROM libraries WHERE id = $1`,
		item.LibraryID).Scan(&language, &country, &sub.Locale.Artwork)
	if err != nil {
		return Subject{}, false, err
	}
	sub.Locale.Language, sub.Locale.Country = deref(language), deref(country)
	rows, err := s.pool.Query(ctx, `SELECT DISTINCT source FROM library_sources WHERE library_id = $1 AND enabled`, item.LibraryID)
	if err == nil {
		sub.Sources, err = pgx.CollectRows(rows, pgx.RowTo[domain.FieldSource])
	}
	if err != nil {
		return Subject{}, false, err
	}
	var provider domain.Provider
	var value string
	rows, err = s.pool.Query(ctx, `SELECT provider, value FROM external_ids WHERE item_id = $1`, id)
	if err == nil {
		_, err = pgx.ForEachRow(rows, []any{&provider, &value}, func() error {
			sub.IDs[provider] = value
			return nil
		})
	}
	if err != nil {
		return Subject{}, false, err
	}
	if item.Kind == domain.ItemShow {
		// Only a season holding an episode still titled by its file name, as Jellyfin asks a
		// provider only about items it has never refreshed: a show's new episode costs one season.
		// A season's own title is always its number, so it says nothing of what was asked.
		rows, err := s.pool.Query(ctx, `
			SELECT DISTINCT s.season_number FROM items s
			JOIN items e ON e.parent_id = s.id
			JOIN item_fields f ON f.item_id = e.id AND f.field = 'title' AND f.source = 'file'
			WHERE s.parent_id = $1 AND s.kind = 'season'
			ORDER BY s.season_number`, id)
		if err == nil {
			sub.Seasons, err = pgx.CollectRows(rows, pgx.RowTo[int])
		}
		if err != nil {
			return Subject{}, false, err
		}
	}
	return sub, true, nil
}

// SaveIdentity writes what a provider says about a title, and about the seasons and episodes of a
// show, under every source that ranks above it, of what its library asks the provider for of each.
func (s *Store) SaveIdentity(ctx context.Context, id uuid.UUID, source domain.FieldSource, m domain.Metadata, seasons map[int]domain.SeasonMetadata) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		item := id
		asks, err := askedOf(ctx, tx, item, source)
		if err != nil {
			return err
		}
		m = asks.of(asks.title, m)
		if err := applyMetadata(ctx, tx, item, source, m); err != nil {
			return err
		}
		if err := saveIDs(ctx, tx, item, domain.IDFromMatch, m.IDs); err != nil {
			return err
		}
		if err := saveRemoteVideos(ctx, tx, item, source, m.Videos); err != nil {
			return err
		}
		if err := saveProviderArtwork(ctx, tx, item, source, m.Artwork); err != nil {
			return err
		}
		if err := saveRatings(ctx, tx, item, source, m.Ratings); err != nil {
			return err
		}
		if err := saveGroupings(ctx, tx, item, source, m.Collections); err != nil {
			return err
		}
		credits := []credited{{item, m.Credits}}
		for number, season := range seasons {
			var seasonID uuid.UUID
			err := tx.QueryRow(ctx, `SELECT id FROM items WHERE parent_id = $1 AND kind = 'season' AND season_number = $2 LIMIT 1`,
				item, number).Scan(&seasonID)
			if errors.Is(err, pgx.ErrNoRows) {
				continue
			}
			if err != nil {
				return err
			}
			said := asks.of(domain.ItemSeason, season.Metadata)
			// A season is named by its number, as the scan names it, whatever a provider calls it
			// ("Season Three", "Book One: Water"); an NFO beside it is the reader's own.
			if source != domain.SourceNFO {
				said.Title, said.SortTitle = "", ""
			}
			if err := applyMetadata(ctx, tx, seasonID, source, said); err != nil {
				return err
			}
			if err := saveProviderArtwork(ctx, tx, seasonID, source, said.Artwork); err != nil {
				return err
			}
			rows, err := tx.Query(ctx, `
				SELECT id, episode_number FROM items WHERE parent_id = $1 AND kind = 'episode' AND episode_number IS NOT NULL`, seasonID)
			if err != nil {
				return err
			}
			type episode struct {
				ID            uuid.UUID
				EpisodeNumber int
			}
			episodes, err := pgx.CollectRows(rows, pgx.RowToStructByName[episode])
			if err != nil {
				return err
			}
			for _, e := range episodes {
				if said, ok := season.Episodes[e.EpisodeNumber]; ok {
					said = asks.of(domain.ItemEpisode, said)
					if err := applyMetadata(ctx, tx, e.ID, source, said); err != nil {
						return err
					}
					if err := saveProviderArtwork(ctx, tx, e.ID, source, said.Artwork); err != nil {
						return err
					}
					if err := saveRatings(ctx, tx, e.ID, source, said.Ratings); err != nil {
						return err
					}
					credits = append(credits, credited{e.ID, said.Credits})
				}
			}
		}
		if err := saveCredits(ctx, tx, source, credits); err != nil {
			return err
		}
		return keyTitle(ctx, tx, item)
	})
}

// saveRemoteVideos replaces what a provider links to for a title with the videos it links to now,
// of the kinds the title's library keeps.
func saveRemoteVideos(ctx context.Context, tx db, item uuid.UUID, source domain.FieldSource, videos []domain.RemoteVideo) error {
	// A video linked again keeps its still's id, so the still is not fetched again.
	stills := map[string]uuid.UUID{}
	var site, key string
	var still uuid.UUID
	rows, err := tx.Query(ctx, `
		SELECT site, key, thumb_id FROM remote_videos WHERE item_id = $1 AND source = $2 AND thumb_id IS NOT NULL`, item, source)
	if err != nil {
		return err
	}
	if _, err := pgx.ForEachRow(rows, []any{&site, &key, &still}, func() error {
		stills[site+"/"+key] = still
		return nil
	}); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM remote_videos WHERE item_id = $1 AND source = $2`, item, source); err != nil {
		return err
	}
	rows, err = tx.Query(ctx, `
		SELECT e.kind FROM library_remote_extras e JOIN items i ON i.library_id = e.library_id WHERE i.id = $1`, item)
	if err != nil {
		return err
	}
	kept, err := pgx.CollectRows(rows, pgx.RowTo[domain.ExtraKind])
	if err != nil {
		return err
	}
	b := &pgx.Batch{}
	for _, v := range videos {
		if !slices.Contains(kept, v.Kind) {
			continue
		}
		var published *time.Time
		if !v.Published.IsZero() {
			published = &v.Published
		}
		var thumb *uuid.UUID
		if videoStill(v.Site, v.Key) != "" {
			id, ok := stills[v.Site+"/"+v.Key]
			if !ok {
				id = uuid.NewV7()
			}
			thumb = &id
		}
		b.Queue(`
			INSERT INTO remote_videos (item_id, source, position, kind, site, key, name, language, published_at, thumb_id)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
			item, source, b.Len(), v.Kind, v.Site, v.Key, v.Name, optional(v.Language), published, thumb)
	}
	if b.Len() == 0 {
		return nil
	}
	return tx.SendBatch(ctx, b).Close()
}
