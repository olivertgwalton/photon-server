package store

import (
	"context"
	"errors"
	"slices"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
)

// Subject is what a film or show is matched to a provider by, with the seasons of a show that
// a provider has yet to describe.
type Subject struct {
	Kind  domain.ItemKind
	Title string
	Year  int
	IDs   map[domain.Provider]string
	// Scope and Seasons are which of a show's seasons it is described with.
	Scope   domain.SeasonScope
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
	// Titles is which title its library gives it.
	Titles domain.TitleLanguage
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
	sub := Subject{Kind: item.Kind, Title: item.Title, Order: item.EpisodeOrder, Unmatched: unmatched}
	if item.Year != nil {
		sub.Year = *item.Year
	}
	// Its own language and country over its library's, as Jellyfin's item settings are.
	var own, lib domain.Locale
	var media domain.LibraryMedia
	err = s.pool.QueryRow(ctx, `
		SELECT coalesce(i.metadata_language, ''), coalesce(i.certification_country, ''),
			coalesce(l.metadata_language, ''), coalesce(l.certification_country, ''), l.artwork_language, l.title_language, l.media
		FROM items i JOIN libraries l ON l.id = i.library_id WHERE i.id = $1`, id).
		Scan(&own.Language, &own.Country, &lib.Language, &lib.Country, &lib.Artwork, &sub.Titles, &media)
	if err != nil {
		return Subject{}, false, err
	}
	sub.Locale = own.Or(lib)
	sub.Sources, err = queryColumn[domain.FieldSource](ctx, s.pool,
		`SELECT DISTINCT source FROM library_sources WHERE library_id = $1 AND enabled`, item.LibraryID)
	if err != nil {
		return Subject{}, false, err
	}
	sub.IDs, err = queryMap[domain.Provider, string](ctx, s.pool, `SELECT provider, value FROM external_ids WHERE item_id = $1`, id)
	if err != nil {
		return Subject{}, false, err
	}
	sub.Scope = domain.SeasonsNumbered
	switch {
	case item.Kind == domain.ItemShow && media == domain.MediaRemote:
		// A remote show has no files to number its seasons by. Its first match is every season the
		// provider has; after, as for a folder show, a provider says what is airing, and the
		// episodes that have aired since are added.
		var seasons int
		if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM items WHERE parent_id = $1 AND kind = 'season'`, id).Scan(&seasons); err != nil {
			return Subject{}, false, err
		}
		if seasons == 0 {
			sub.Scope = domain.SeasonsEvery
		}
	case item.Kind == domain.ItemShow:
		// Only a season holding an episode still titled by its file name, as Jellyfin asks a
		// provider only about items it has never refreshed: a show's new episode costs one season.
		// A season's own title is always its number, so it says nothing of what was asked.
		sub.Seasons, err = queryColumn[int](ctx, s.pool, `
			SELECT DISTINCT s.season_number FROM items s
			JOIN items e ON e.parent_id = s.id
			JOIN item_fields f ON f.item_id = e.id AND f.field = 'title' AND f.source = 'file'
			WHERE s.parent_id = $1 AND s.kind = 'season'
			ORDER BY s.season_number`, id)
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
		var rank map[domain.FieldSource]int
		if len(seasons) > 0 {
			if rank, err = episodeRanks(ctx, tx, item); err != nil {
				return err
			}
		}
		media, err := mediaOf(ctx, tx, item)
		if err != nil {
			return err
		}
		for number, season := range seasons {
			if err := announce(ctx, tx, item, source, rank, number, season.Episodes); err != nil {
				return err
			}
			if _, ranked := rank[source]; ranked && media == domain.MediaRemote {
				if err := addAired(ctx, tx, item, number, season.Episodes); err != nil {
					return err
				}
			}
			seasonID, err := seasonOf(ctx, tx, item, number)
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
			type episode struct {
				ID            uuid.UUID
				EpisodeNumber int
			}
			episodes, err := queryStructs[episode](ctx, tx, `
				SELECT id, episode_number FROM items WHERE parent_id = $1 AND kind = 'episode' AND episode_number IS NOT NULL`, seasonID)
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

// episodeRanks is how highly a show's library ranks each source for its episodes, as ranks is.
func episodeRanks(ctx context.Context, tx db, show uuid.UUID) (map[domain.FieldSource]int, error) {
	row := &model.Item{ID: show, Kind: domain.ItemEpisode}
	if err := tx.QueryRow(ctx, `SELECT library_id FROM items WHERE id = $1`, show).Scan(&row.LibraryID); err != nil {
		return nil, found(err)
	}
	taken, err := rankings(ctx, tx, []*model.Item{row}, domain.FetcherMetadata)
	if err != nil {
		return nil, err
	}
	return rankOf(taken[show]), nil
}

// announce keeps the episodes a source lists in a season of a show, each with the day it airs where
// it says, in place of what was kept of the season, unless a source the show's library ranks above
// this one for episodes said it.
func announce(ctx context.Context, tx db, show uuid.UUID, source domain.FieldSource, rank map[domain.FieldSource]int, season int, episodes map[int]domain.Metadata) error {
	if _, ok := rank[source]; !ok {
		return nil
	}
	var by domain.FieldSource
	err := tx.QueryRow(ctx, `SELECT source FROM announced_episodes WHERE show_id = $1 AND season_number = $2 LIMIT 1`, show, season).Scan(&by)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if err == nil && rank[by] > rank[source] {
		return nil
	}
	if _, err := tx.Exec(ctx, `DELETE FROM announced_episodes WHERE show_id = $1 AND season_number = $2`, show, season); err != nil {
		return err
	}
	b := &pgx.Batch{}
	for number, e := range episodes {
		var aired *time.Time
		if !e.ReleaseDate.IsZero() {
			aired = &e.ReleaseDate
		}
		b.Queue(`
			INSERT INTO announced_episodes (show_id, season_number, episode_number, source, title, overview, air_date)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			show, season, number, source, e.Title, e.Overview, aired)
	}
	if b.Len() == 0 {
		return nil
	}
	return tx.SendBatch(ctx, b).Close()
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
	kept, err := queryColumn[domain.ExtraKind](ctx, tx, `
		SELECT e.kind FROM library_remote_extras e JOIN items i ON i.library_id = e.library_id WHERE i.id = $1`, item)
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
