package store

import (
	"context"
	"errors"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// ErrNothingOnDisk refuses to analyse a title none of whose files is on disk.
var ErrNothingOnDisk = errors.New("nothing of it is on disk to analyse")

// AnalyseTitle asks for every file of a title on disk to be read again, as Plex's Analyze is: a
// film's or episode's own, and a show's or season's episodes', not their extras. What is made from
// a file's streams follows each as it is read.
func (s *Store) AnalyseTitle(ctx context.Context, id uuid.UUID) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := readItem(ctx, tx, id); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `
			WITH RECURSIVE under AS (
				SELECT id FROM items WHERE id = $1
				UNION ALL
				SELECT i.id FROM items i JOIN under u ON i.parent_id = u.id WHERE i.kind IN ('season', 'episode')
			)
			SELECT p.id FROM parts p JOIN versions v ON v.id = p.version_id
			WHERE v.item_id IN (SELECT id FROM under) AND v.missing_since IS NULL`, id)
		if err != nil {
			return err
		}
		parts, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		if err != nil {
			return err
		}
		if len(parts) == 0 {
			return ErrNothingOnDisk
		}
		for _, part := range parts {
			if err := insertJob(ctx, tx, domain.JobProbe, part, 0, askedPriority, domain.JobDueNow); err != nil {
				return err
			}
		}
		return nil
	})
}

// SaveProbe replaces what a part's file was read to hold, and its copy's facts with it, then asks
// again for what is made from its streams, as its library makes them: its keyframes, its previews
// and, for an episode, its season's markers. An admin asked, so none waits for the window.
func (s *Store) SaveProbe(ctx context.Context, part uuid.UUID, f domain.Facts) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var version, lib uuid.UUID
		var idx int16
		var kind domain.ItemKind
		var season *uuid.UUID
		err := tx.QueryRow(ctx, `
			SELECT p.version_id, v.library_id, p.idx, i.kind, i.parent_id
			FROM parts p JOIN versions v ON v.id = p.version_id JOIN items i ON i.id = v.item_id WHERE p.id = $1`,
			part).Scan(&version, &lib, &idx, &kind, &season)
		if err != nil {
			return found(err)
		}
		for _, stmt := range []string{
			`DELETE FROM streams WHERE part_id = $1`,
			`DELETE FROM chapters WHERE part_id = $1`,
		} {
			if _, err := tx.Exec(ctx, stmt, part); err != nil {
				return err
			}
		}
		if err := saveFacts(ctx, tx, part, &f); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE parts SET duration_ms = $2 WHERE id = $1`, part, f.Duration.Milliseconds()); err != nil {
			return err
		}
		// A part's length moves every later part's place on the copy's timeline, and the copy's own.
		for _, stmt := range []string{
			`UPDATE parts p SET offset_ms = coalesce((
				SELECT sum(q.duration_ms) FROM parts q WHERE q.version_id = p.version_id AND q.idx < p.idx), 0)
			WHERE p.version_id = $1`,
			`UPDATE versions v SET duration_ms = t.ms, bitrate_kbps = CASE WHEN t.ms > 0 THEN v.size_bytes * 8 / t.ms ELSE 0 END
			FROM (SELECT sum(duration_ms) AS ms FROM parts WHERE version_id = $1) t WHERE v.id = $1`,
		} {
			if _, err := tx.Exec(ctx, stmt, version); err != nil {
				return err
			}
		}
		video := firstVideo(&f)
		if idx == 0 {
			var width, height *int
			var codec *string
			var videoRange *domain.Range
			var dv *int16
			if video != nil {
				width, height, codec, videoRange = &video.Width, &video.Height, &video.Codec, &video.Range
				if video.DolbyVision != nil {
					profile := int16(video.DolbyVision.Profile)
					dv = &profile
				}
			}
			_, err := tx.Exec(ctx, `
				UPDATE versions SET container = $2, width = $3, height = $4, video_codec = $5, video_range = $6, dv_profile = $7
				WHERE id = $1`, version, f.Container, width, height, codec, videoRange, dv)
			if err != nil {
				return err
			}
		}
		settings, err := analysisOf(ctx, tx, lib)
		if err != nil {
			return err
		}
		return queueAnalysis(ctx, tx, part, kind, season, video != nil, settings)
	})
}

// queueAnalysis forgets what was made from a part's streams and asks for it again.
func queueAnalysis(ctx context.Context, tx db, part uuid.UUID, kind domain.ItemKind, season *uuid.UUID, video bool, settings analysis) error {
	if video && settings.Keyframes != domain.KeyframesOff {
		if _, err := tx.Exec(ctx, `DELETE FROM keyframes WHERE part_id = $1`, part); err != nil {
			return err
		}
		if err := insertJob(ctx, tx, domain.JobKeyframes, part, 0, indexPriority, domain.JobDueNow); err != nil {
			return err
		}
	}
	if video && settings.Previews != domain.PreviewsOff {
		if _, err := tx.Exec(ctx, `DELETE FROM previews WHERE part_id = $1`, part); err != nil {
			return err
		}
		if err := insertJob(ctx, tx, domain.JobPreviews, part, 0, askedPriority, domain.JobDueNow); err != nil {
			return err
		}
	}
	if kind == domain.ItemEpisode && season != nil && settings.Markers == domain.MarkersAll {
		if _, err := tx.Exec(ctx, `UPDATE parts SET fingerprinted_at = NULL WHERE id = $1`, part); err != nil {
			return err
		}
		return insertJob(ctx, tx, domain.JobMarkers, *season, 0, askedPriority, domain.JobDueNow)
	}
	return nil
}
