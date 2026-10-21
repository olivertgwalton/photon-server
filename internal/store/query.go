package store

import (
	"context"
	"strings"
	"uuid"

	"github.com/jackc/pgx/v5"
	"golang.org/x/sync/errgroup"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
)

// itemColumns are model.Item's, for a statement that reads whole items.
const itemColumns = `id, library_id, kind, title, sort_title, year, folder, added_at, parent_id, season_number,
	episode_number, episode_end, air_date, extra_kind, scan_title, original_title, overview, tagline, certificate,
	release_date, genres, studios, episode_order, episode_group, airs_before_season, airs_before_episode, airs_after_season`

// itemColumnsOf is itemColumns read through alias, for a statement that joins items to others.
func itemColumnsOf(alias string) string {
	cols := strings.Split(itemColumns, ",")
	for n, c := range cols {
		cols[n] = alias + "." + strings.TrimSpace(c)
	}
	return strings.Join(cols, ", ")
}

// episodeOrder is the order a show's episodes play in, read through alias: by the season each aired
// in and its place there, one with no number after those with one, then by air date and title. A
// special its source places, as Jellyfin's do, is in the season it aired in: before the episode
// named, at the season's start where none is, or after its last; one placed nowhere is in the
// specials. Where the specials go is each statement's own choice.
func episodeOrder(alias string) []string {
	return []string{
		airedSeason(alias), alias + ".episode_number IS NULL",
		"CASE WHEN " + alias + ".airs_after_season IS NOT NULL THEN 32768 WHEN " + alias + ".airs_before_season IS NOT NULL THEN " +
			"coalesce(" + alias + ".airs_before_episode, 1) - 0.5 ELSE coalesce(" + alias + ".episode_number, 0) END",
		"coalesce(" + alias + ".episode_number, 0)",
		alias + ".air_date IS NULL", "coalesce(" + alias + ".air_date, 'epoch')", alias + ".sort_title", alias + ".id",
	}
}

// airedSeason is the season an episode is watched in, read through alias: the one a special aired
// in, where its source places it, else its own.
func airedSeason(alias string) string {
	return "coalesce(" + alias + ".airs_after_season, " + alias + ".airs_before_season, " + alias + ".season_number)"
}

// seasonEpisodes joins to here, a season, its episodes as e as they are watched: its own and the
// specials placed in it, read from it and the specials alone; or, of the specials, their own.
const seasonEpisodes = `JOIN items s ON s.parent_id = here.parent_id AND s.kind = 'season'
		AND (s.id = here.id OR s.season_number = 0 AND here.season_number > 0)
	JOIN items e ON e.parent_id = s.id AND e.kind = 'episode' AND (s.id = here.id OR ` + `coalesce(e.airs_after_season, e.airs_before_season, e.season_number) = here.season_number)`

// seasonOrder is the order of seasonEpisodes, ascending: as they are watched, or, of the specials,
// as their files number them.
func seasonOrder() string {
	aired := episodeOrder("e")
	filed := map[int]string{0: "e.season_number", 2: "coalesce(e.episode_number, 0)"}
	keys := make([]string, len(aired))
	for i, key := range aired {
		keys[i] = key
		if f, ok := filed[i]; ok {
			keys[i] = "CASE WHEN here.season_number = 0 THEN " + f + " ELSE " + key + " END"
		}
	}
	return strings.Join(keys, ", ")
}

// episodeOrderBy is episodeOrder, read through e, as columns to sort by, each in dir: a row of them
// sorts slower.
func episodeOrderBy(dir string) string {
	return strings.Join(episodeOrder("e"), " "+dir+", ") + " " + dir
}

// episodeOrderOf is episodeOrder as one row, to compare two episodes by.
func episodeOrderOf(alias string) string {
	return "(" + strings.Join(episodeOrder(alias), ", ") + ")"
}

// The columns of the other rows read whole.
const (
	versionColumns = `id, item_id, library_id, fingerprint, edition, label, container, width, height, video_codec,
		video_range, dv_profile, bitrate_kbps, size_bytes, duration_ms, missing_since`
	partColumns   = `id, version_id, idx, size_bytes, duration_ms, offset_ms`
	streamColumns = `part_id, idx, kind, codec, profile, language, title, is_default, forced, hearing_impaired,
		commentary, width, height, frame_rate, bit_depth, level, video_range, interlaced, dv_profile, dv_level,
		dv_compatibility, channels, channel_layout, sample_rate, bitrate_kbps`
	chapterColumns      = `part_id, idx, start_ms, end_ms, title`
	markerColumns       = `part_id, kind, source, start_ms, end_ms`
	subtitleFileColumns = `id, version_id, codec, language, title, forced, is_default, hearing_impaired`
)

// counted runs page and, where a total is asked for, counts with count the rows it is taken from,
// on a connection each, so a page costs the longer of the two rather than both; and answers the
// count, nil where none was asked for, as by a page that says nothing of counting.
func (s *Store) counted(ctx context.Context, counting domain.Counting, count string, args pgx.NamedArgs, page func(ctx context.Context) error) (*int64, error) {
	if counting != domain.CountExact {
		return nil, page(ctx)
	}
	var total int64
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error { return s.pool.QueryRow(gctx, count, args).Scan(&total) })
	g.Go(func() error { return page(gctx) })
	return &total, g.Wait()
}

// readRow answers the one row a statement finds, each column into the field of its name, or
// ErrNotFound.
func readRow[T any](ctx context.Context, q db, sql string, args ...any) (T, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return *new(T), err
	}
	row, err := pgx.CollectOneRow(rows, pgx.RowToStructByName[T])
	return row, found(err)
}

// queryRows answers the rows a statement finds, each column into the field of its name.
func queryRows[T any](ctx context.Context, q db, sql string, args ...any) ([]*T, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToAddrOfStructByName[T])
}

// queryStructs is queryRows by value.
func queryStructs[T any](ctx context.Context, q db, sql string, args ...any) ([]T, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByName[T])
}

// queryColumn answers the one column of the rows a statement finds.
func queryColumn[T any](ctx context.Context, q db, sql string, args ...any) ([]T, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[T])
}

// queryMap answers a statement's rows of two columns, a key and its value.
func queryMap[K comparable, V any](ctx context.Context, q db, sql string, args ...any) (map[K]V, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	out := map[K]V{}
	var k K
	var v V
	_, err = pgx.ForEachRow(rows, []any{&k, &v}, func() error {
		out[k] = v
		return nil
	})
	return out, err
}

// querySet answers the one column of the rows a statement finds, as a set.
func querySet[K comparable](ctx context.Context, q db, sql string, args ...any) (map[K]bool, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	out := map[K]bool{}
	var k K
	_, err = pgx.ForEachRow(rows, []any{&k}, func() error {
		out[k] = true
		return nil
	})
	return out, err
}

// ids is the items' ids, for an array parameter.
func ids(rows []*model.Item) []uuid.UUID {
	out := make([]uuid.UUID, len(rows))
	for n, r := range rows {
		out[n] = r.ID
	}
	return out
}

func deref[T any](p *T) T {
	if p == nil {
		var zero T
		return zero
	}
	return *p
}

func first(ids []uuid.UUID) uuid.UUID {
	if len(ids) == 0 {
		return uuid.UUID{}
	}
	return ids[0]
}

// hasRepeats is whether any id comes more than once.
func hasRepeats(ids []uuid.UUID) bool {
	seen := map[uuid.UUID]bool{}
	for _, id := range ids {
		if seen[id] {
			return true
		}
		seen[id] = true
	}
	return false
}
