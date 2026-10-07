package store

import (
	"context"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// Trickplay is how a part's thumbnail sheets are laid out: each thumbnail Width by Height, one
// every IntervalMS from the part's start, Columns by Rows to a sheet left to right then down, and
// Thumbnails in all.
type Trickplay struct {
	Width      int
	Height     int
	IntervalMS int
	Columns    int
	Rows       int
	Thumbnails int
	Sheets     int
}

// ChapterSpan is a chapter on its part's own timeline.
type ChapterSpan struct {
	Idx        int
	Start, End time.Duration
}

// PreviewSource is what making a part's previews needs: what its library asks for, its picture's
// range, its length and its chapters, or one spanning the part where it has none.
type PreviewSource struct {
	Level    domain.PreviewLevel
	Range    domain.Range
	Length   time.Duration
	Chapters []ChapterSpan
}

// PreviewSource answers what a part's previews are made from, or ErrNotFound for a part gone.
func (s *Store) PreviewSource(ctx context.Context, part uuid.UUID) (PreviewSource, error) {
	var src PreviewSource
	var level string
	var vr *string
	var length int64
	err := s.pool.QueryRow(ctx, `
		SELECT l.previews, (SELECT video_range FROM streams WHERE part_id = p.id AND kind = 'video' ORDER BY idx LIMIT 1), p.duration_ms
		FROM parts p JOIN versions v ON v.id = p.version_id JOIN libraries l ON l.id = v.library_id
		WHERE p.id = $1`, part).Scan(&level, &vr, &length)
	if err != nil {
		return src, found(err)
	}
	src.Level, src.Range, src.Length = domain.PreviewLevel(level), domain.Range(deref(vr)), time.Duration(length)*time.Millisecond
	rows, err := s.pool.Query(ctx, `SELECT idx, start_ms, end_ms FROM chapters WHERE part_id = $1 ORDER BY idx`, part)
	if err != nil {
		return src, err
	}
	src.Chapters, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (ChapterSpan, error) {
		var c ChapterSpan
		var start, end int64
		err := r.Scan(&c.Idx, &start, &end)
		c.Start, c.End = time.Duration(start)*time.Millisecond, time.Duration(end)*time.Millisecond
		return c, err
	})
	// A part with no chapters is pictured as one, as Jellyfin gives a video with none its own, so
	// an extra's card has a still.
	if err == nil && len(src.Chapters) == 0 {
		src.Chapters = []ChapterSpan{{End: src.Length}}
	}
	return src, err
}

// SavePreviews records the previews made of a part: the chapters with an image, and its trickplay
// sheets where t is not nil.
func (s *Store) SavePreviews(ctx context.Context, part uuid.UUID, chapters []int, t *Trickplay) error {
	if chapters == nil {
		chapters = []int{}
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO previews (part_id, chapter_images) VALUES ($1, $2)
			ON CONFLICT (part_id) DO UPDATE SET chapter_images = excluded.chapter_images`, part, chapters)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM trickplay WHERE part_id = $1`, part); err != nil || t == nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO trickplay (part_id, width, height, interval_ms, columns, rows, thumbnails)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			part, t.Width, t.Height, t.IntervalMS, t.Columns, t.Rows, t.Thumbnails)
		return err
	})
}

// ForgetPreviews records that a part has no previews.
func (s *Store) ForgetPreviews(ctx context.Context, part uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM previews WHERE part_id = $1`, part)
	return err
}

// ForgetMissingPreviews forgets the previews of parts on no disk since before, answering how many.
// Their folders go with the next Sweep.
func (s *Store) ForgetMissingPreviews(ctx context.Context, before time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM previews pv USING parts p JOIN versions v ON v.id = p.version_id
		WHERE pv.part_id = p.id AND v.missing_since < $1
			AND NOT EXISTS (SELECT 1 FROM part_files f WHERE f.part_id = p.id)`, before)
	return tag.RowsAffected(), err
}

// Trickplay answers a part's thumbnail sheets, or ErrNotFound where it has none or the profile may
// not see its title.
func (s *Store) Trickplay(ctx context.Context, profile, part uuid.UUID) (Trickplay, error) {
	var w, h, interval, cols, rows, n int
	err := s.pool.QueryRow(ctx, `
		SELECT t.width, t.height, t.interval_ms, t.columns, t.rows, t.thumbnails
		FROM trickplay t JOIN parts p ON p.id = t.part_id JOIN versions v ON v.id = p.version_id
		JOIN items i ON i.id = v.item_id, viewer($2) asking
		WHERE t.part_id = $1 AND sees(asking, i)`, part, profile).
		Scan(&w, &h, &interval, &cols, &rows, &n)
	if err != nil {
		return Trickplay{}, found(err)
	}
	return sheetsOf(w, h, interval, cols, rows, n), nil
}

func sheetsOf(width, height, intervalMS, columns, rows, thumbnails int) Trickplay {
	return Trickplay{
		Width: width, Height: height, IntervalMS: intervalMS, Columns: columns, Rows: rows,
		Thumbnails: thumbnails, Sheets: (thumbnails + columns*rows - 1) / (columns * rows),
	}
}

// QueuePreviews queues every part on disk whose previews are not what its library asks for: none
// made yet, made before its library asked for more or less, or left by a job that died, due as
// said; due now, every previews job already queued is due now too. It answers how many.
func (s *Store) QueuePreviews(ctx context.Context, due domain.JobDue) (int64, error) {
	var n int64
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := promoteFor(ctx, tx, domain.JobPreviews, due); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `
		INSERT INTO jobs (kind, subject, due)
		SELECT 'previews', p.id, $1 FROM parts p JOIN versions v ON v.id = p.version_id JOIN libraries l ON l.id = v.library_id
		WHERE EXISTS (SELECT 1 FROM part_files f WHERE f.part_id = p.id)
			AND EXISTS (SELECT 1 FROM streams s WHERE s.part_id = p.id AND s.kind = 'video')
			AND l.previews <> CASE
				WHEN EXISTS (SELECT 1 FROM trickplay t WHERE t.part_id = p.id) THEN 'all'
				WHEN EXISTS (SELECT 1 FROM previews pv WHERE pv.part_id = p.id) THEN 'chapters'
				ELSE 'off' END
		ON CONFLICT (kind, subject) DO UPDATE SET
			state = CASE jobs.state WHEN 'running' THEN 'rerun' WHEN 'dead' THEN 'queued' ELSE jobs.state END,
			attempts = CASE jobs.state WHEN 'dead' THEN 0 ELSE jobs.attempts END,
			due = CASE jobs.state WHEN 'dead' THEN excluded.due ELSE jobs.due END`, due)
		n = tag.RowsAffected()
		return err
	})
	return n, err
}

// LivePreviews answers which of these parts have previews recorded.
func (s *Store) LivePreviews(ctx context.Context, parts []uuid.UUID) (map[uuid.UUID]bool, error) {
	return querySet[uuid.UUID](ctx, s.pool, `SELECT part_id FROM previews WHERE part_id = ANY($1)`, parts)
}
