package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/olivertgwalton/photon-server/internal/animelist"
	"github.com/olivertgwalton/photon-server/internal/domain"
)

const animeEntryColumns = `anidb_id, coalesce(anilist_id, 0), coalesce(tvdb_show, 0), tvdb_season, tvdb_offset,
	coalesce(tmdb_show, 0), tmdb_season, tmdb_offset, tmdb_movies, imdb_ids, mappings`

// ReplaceAnimeEntries keeps the anime lists' entries in place of those kept before.
func (s *Store) ReplaceAnimeEntries(ctx context.Context, entries []animelist.Entry) error {
	rows := make([][]any, len(entries))
	for i, e := range entries {
		mappings, err := json.Marshal(e.Mappings)
		if err != nil {
			return err
		}
		rows[i] = []any{
			e.AniDB, nullable(e.AniList), nullable(e.TVDB.Show), e.TVDB.Season, e.TVDB.Offset,
			nullable(e.TMDB.Show), e.TMDB.Season, e.TMDB.Offset, orEmpty(e.TMDBMovies), orEmpty(e.IMDb), mappings,
		}
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM anime_entries`); err != nil {
			return err
		}
		_, err := tx.CopyFrom(ctx, pgx.Identifier{"anime_entries"}, []string{
			"anidb_id", "anilist_id", "tvdb_show", "tvdb_season", "tvdb_offset",
			"tmdb_show", "tmdb_season", "tmdb_offset", "tmdb_movies", "imdb_ids", "mappings",
		}, pgx.CopyFromRows(rows))
		return err
	})
}

// AnimeShow answers the entries the anime lists place in a show on TheTVDB or TMDB.
func (s *Store) AnimeShow(ctx context.Context, p domain.Provider, show int) ([]animelist.Entry, error) {
	column := map[domain.Provider]string{domain.ProviderTVDB: "tvdb_show", domain.ProviderTMDB: "tmdb_show"}[p]
	if column == "" {
		return nil, fmt.Errorf("anime lists place no show on %s", p)
	}
	return animeEntries(ctx, s.pool, `SELECT `+animeEntryColumns+` FROM anime_entries WHERE `+column+` = $1 ORDER BY anidb_id`, show)
}

// AnimeEntry answers the entry an id names: its AniDB or AniList id, or a film's TMDB or IMDb id;
// false for one the lists do not name.
func (s *Store) AnimeEntry(ctx context.Context, p domain.Provider, id string) (animelist.Entry, bool, error) {
	where := map[domain.Provider]string{
		domain.ProviderAniDB: "anidb_id = $1::integer", domain.ProviderAniList: "anilist_id = $1::integer",
		domain.ProviderTMDB: "$1::integer = ANY(tmdb_movies)", domain.ProviderIMDb: "$1 = ANY(imdb_ids)",
	}[p]
	if where == "" {
		return animelist.Entry{}, false, fmt.Errorf("anime lists name no entry by a %s id", p)
	}
	if _, err := strconv.Atoi(id); err != nil && p != domain.ProviderIMDb {
		return animelist.Entry{}, false, nil
	}
	entries, err := animeEntries(ctx, s.pool, `SELECT `+animeEntryColumns+` FROM anime_entries WHERE `+where+` ORDER BY anidb_id LIMIT 1`, id)
	if err != nil || len(entries) == 0 {
		return animelist.Entry{}, false, err
	}
	return entries[0], true, nil
}

func animeEntries(ctx context.Context, q db, sql string, args ...any) ([]animelist.Entry, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (animelist.Entry, error) {
		var e animelist.Entry
		var mappings []byte
		err := row.Scan(&e.AniDB, &e.AniList, &e.TVDB.Show, &e.TVDB.Season, &e.TVDB.Offset,
			&e.TMDB.Show, &e.TMDB.Season, &e.TMDB.Offset, &e.TMDBMovies, &e.IMDb, &mappings)
		if err == nil {
			err = json.Unmarshal(mappings, &e.Mappings)
		}
		return e, err
	})
}

// nullable is a number the lists leave at 0 for none, NULL so.
func nullable(n int) *int {
	if n == 0 {
		return nil
	}
	return &n
}

func orEmpty[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
