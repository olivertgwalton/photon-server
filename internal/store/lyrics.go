package store

import (
	"context"
	"errors"
	"strings"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
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

// KeptLyrics answers what source said of a track's lyrics, "" for none or for a source not asked,
// and whether that answer is current: lyrics are until a refresh asks again, and none until keepNone
// has passed, so the source is asked again.
func (s *Store) KeptLyrics(ctx context.Context, item uuid.UUID, source domain.FieldSource) (string, bool, error) {
	var lyrics *string
	var current bool
	err := s.pool.QueryRow(ctx, `
		SELECT lyrics, `+currentLyrics+` FROM fetched_lyrics f WHERE item_id = $1 AND source = $2`,
		item, source, keepNone).Scan(&lyrics, &current)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	return deref(lyrics), current, err
}

// currentLyrics is whether a kept answer f stands, keepNone being $3: lyrics until a refresh marks
// them asked again, none until keepNone has passed.
const currentLyrics = `(f.lyrics IS NOT NULL AND f.fetched_at > '-infinity' OR f.fetched_at > now() - $3::interval)`

// KeepLyrics keeps what source said of a track's lyrics, and how they are timed: "" for none.
func (s *Store) KeepLyrics(ctx context.Context, item uuid.UUID, source domain.FieldSource, lyrics string, timing domain.LyricsTiming) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO fetched_lyrics (item_id, source, lyrics, timing) VALUES ($1, $2, $3, $4)
		ON CONFLICT (item_id, source) DO UPDATE SET lyrics = excluded.lyrics, timing = excluded.timing, fetched_at = now()`,
		item, source, optional(lyrics), optional(timing))
	return err
}

// AskLyrics queues a fetch of a track's lyrics where askLyrics would.
func (s *Store) AskLyrics(ctx context.Context, item uuid.UUID) error {
	return askLyrics(ctx, s.pool, `@id`, pgx.NamedArgs{"id": item})
}

// askLyrics queues a lyrics fetch for each of the tracks items selects whose own file carries none,
// none of whose library's sources for them has found any, and one of which has not answered, or
// answered none so long ago it is asked again.
func askLyrics(ctx context.Context, tx db, items string, args pgx.NamedArgs) error {
	args["keepNone"] = keepNone
	_, err := tx.Exec(ctx, `
		INSERT INTO jobs (kind, subject)
		SELECT 'lyrics', i.id FROM items i
		WHERE i.id IN (`+items+`) AND i.kind = 'track'
			AND NOT EXISTS (SELECT 1 FROM versions v WHERE v.item_id = i.id AND v.lyrics IS NOT NULL)
			AND NOT EXISTS (SELECT 1 FROM fetched_lyrics f JOIN library_sources s ON s.source = f.source
				WHERE f.item_id = i.id AND f.lyrics IS NOT NULL AND f.fetched_at > '-infinity'
					AND s.library_id = i.library_id AND s.item_kind = 'track' AND s.fetcher = 'lyrics' AND s.enabled)
			AND EXISTS (SELECT 1 FROM library_sources s
				WHERE s.library_id = i.library_id AND s.item_kind = 'track' AND s.fetcher = 'lyrics' AND s.enabled
					AND NOT EXISTS (SELECT 1 FROM fetched_lyrics f WHERE f.item_id = i.id AND f.source = s.source
						AND `+strings.ReplaceAll(currentLyrics, "$3", "@keepNone")+`))`+requeue, args)
	return err
}

// askLyricsAgain marks what was kept of the lyrics of the tracks items selects asked again, so they
// stand until their sources answer anew, and queues their fetch.
func askLyricsAgain(ctx context.Context, tx db, items string, args pgx.NamedArgs) error {
	_, err := tx.Exec(ctx, `UPDATE fetched_lyrics SET fetched_at = '-infinity' WHERE item_id IN (`+items+`)`, args)
	if err != nil {
		return err
	}
	return askLyrics(ctx, tx, items, args)
}

// lyricsTimings answers how the lyrics of each track among rows that has any are timed: its own
// file's, else those of the first of its library's sources for them that found any.
func (s *Store) lyricsTimings(ctx context.Context, rows []*model.Item) (map[uuid.UUID]domain.LyricsTiming, error) {
	var tracks []uuid.UUID
	for _, r := range rows {
		if r.Kind == domain.ItemTrack {
			tracks = append(tracks, r.ID)
		}
	}
	if len(tracks) == 0 {
		return nil, nil
	}
	return queryMap[uuid.UUID, domain.LyricsTiming](ctx, s.pool, `
		SELECT id, timing FROM (SELECT i.id, coalesce(
			(SELECT v.lyrics FROM versions v WHERE v.item_id = i.id AND v.missing_since IS NULL AND v.lyrics IS NOT NULL
				ORDER BY v.lyrics = 'line' DESC LIMIT 1),
			(SELECT f.timing FROM fetched_lyrics f JOIN library_sources s ON s.source = f.source
				WHERE f.item_id = i.id AND f.timing IS NOT NULL AND s.library_id = i.library_id
					AND s.item_kind = 'track' AND s.fetcher = 'lyrics' AND s.enabled
				ORDER BY s.position LIMIT 1)) AS timing
		FROM items i WHERE i.id = ANY($1)) t WHERE timing IS NOT NULL`, tracks)
}
