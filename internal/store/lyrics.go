package store

import (
	"context"
	"errors"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// keepNone is how long a source that had no lyrics for a track is believed: LRCLIB gains lyrics
// as people add them.
const keepNone = 30 * 24 * time.Hour

// LyricsAsk is a track as the sources its library ranks for its lyrics are asked of it: its first
// artist, else its album's, its title, its album, and how long its first version on disk runs; and
// those sources, enabled and in their order.
type LyricsAsk struct {
	Query   domain.LyricsQuery
	Sources []domain.FieldSource
}

// LyricsAsk answers what a track's lyrics are asked by. ErrNotFound for no such track.
func (s *Store) LyricsAsk(ctx context.Context, item uuid.UUID) (LyricsAsk, error) {
	var ask LyricsAsk
	var lib uuid.UUID
	var ms int64
	err := s.pool.QueryRow(ctx, `
		SELECT t.library_id, t.title, coalesce(t.artists->>0, r.title, ''), coalesce(a.title, ''),
			coalesce((SELECT duration_ms FROM versions v WHERE v.item_id = t.id AND v.missing_since IS NULL
				ORDER BY v.id LIMIT 1), 0)
		FROM items t LEFT JOIN items a ON a.id = t.parent_id LEFT JOIN items r ON r.id = a.parent_id
		WHERE t.id = $1 AND t.kind = 'track'`, item).Scan(&lib, &ask.Query.Title, &ask.Query.Artist, &ask.Query.Album, &ms)
	if errors.Is(err, pgx.ErrNoRows) {
		return LyricsAsk{}, ErrNotFound
	}
	if err != nil {
		return LyricsAsk{}, err
	}
	ask.Query.Duration = time.Duration(ms) * time.Millisecond
	ask.Sources, err = queryColumn[domain.FieldSource](ctx, s.pool, `
		SELECT source FROM library_sources
		WHERE library_id = $1 AND item_kind = 'track' AND fetcher = 'lyrics' AND enabled ORDER BY position`, lib)
	return ask, err
}

// KeptLyrics answers what source said of a track's lyrics, and whether it was asked: "" for none,
// which is forgotten after keepNone so the source is asked again.
func (s *Store) KeptLyrics(ctx context.Context, item uuid.UUID, source domain.FieldSource) (string, bool, error) {
	var lyrics *string
	err := s.pool.QueryRow(ctx, `
		SELECT lyrics FROM fetched_lyrics
		WHERE item_id = $1 AND source = $2 AND (lyrics IS NOT NULL OR fetched_at > now() - $3::interval)`,
		item, source, keepNone).Scan(&lyrics)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	return deref(lyrics), err == nil, err
}

// KeepLyrics keeps what source said of a track's lyrics: "" for none.
func (s *Store) KeepLyrics(ctx context.Context, item uuid.UUID, source domain.FieldSource, lyrics string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO fetched_lyrics (item_id, source, lyrics) VALUES ($1, $2, $3)
		ON CONFLICT (item_id, source) DO UPDATE SET lyrics = excluded.lyrics, fetched_at = now()`,
		item, source, optional(lyrics))
	return err
}
