package store

import (
	"context"
	"strings"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/olivertgwalton/photon-server/internal/store/model"
)

// itemColumns are model.Item's, for a statement that reads whole items.
const itemColumns = `id, library_id, kind, title, sort_title, year, folder, added_at, parent_id, season_number,
	episode_number, episode_end, air_date, extra_kind, scan_title, original_title, overview, tagline, certificate,
	release_date, genres, studios, episode_order`

// itemColumnsOf is itemColumns read through alias, for a statement that joins items to others.
func itemColumnsOf(alias string) string {
	cols := strings.Split(itemColumns, ",")
	for n, c := range cols {
		cols[n] = alias + "." + strings.TrimSpace(c)
	}
	return strings.Join(cols, ", ")
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
