package store

import (
	"context"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// SegmentsAsk is what a film's or an episode's parts not yet asked about are asked of the
// providers that time intros and credits: the title, and the length of each part that is a whole
// copy. A copy in several parts is read and not asked about, as a timing is of a whole release.
type SegmentsAsk struct {
	Title domain.SegmentQuery
	Parts map[uuid.UUID]time.Duration
	Read  []uuid.UUID
}

// SegmentsAsk answers what a film or an episode is asked about by, or ErrNotFound.
func (s *Store) SegmentsAsk(ctx context.Context, item uuid.UUID) (SegmentsAsk, error) {
	out := SegmentsAsk{Parts: map[uuid.UUID]time.Duration{}}
	var show *uuid.UUID
	var season, episode *int
	err := s.pool.QueryRow(ctx, `
		SELECT i.kind, i.season_number, i.episode_number, s.parent_id FROM items i LEFT JOIN items s ON s.id = i.parent_id
		WHERE i.id = $1 AND i.kind IN ('movie', 'episode')`, item).Scan(&out.Title.Kind, &season, &episode, &show)
	if err != nil {
		return out, found(err)
	}
	of := item
	if out.Title.Kind == domain.ItemEpisode && show != nil && season != nil && episode != nil {
		of, out.Title.Season, out.Title.Episode = *show, *season, *episode
	}
	ids, err := s.ExternalIDs(ctx, []uuid.UUID{of})
	if err != nil {
		return out, err
	}
	out.Title.IDs = ids[of]
	rows, err := s.pool.Query(ctx, `
		SELECT p.id, p.duration_ms, (SELECT count(*) FROM parts q WHERE q.version_id = p.version_id) = 1
		FROM parts p JOIN versions v ON v.id = p.version_id AND v.missing_since IS NULL
		WHERE v.item_id = $1 AND p.segments_asked_at IS NULL`, item)
	if err != nil {
		return out, err
	}
	var part uuid.UUID
	var ms int64
	var whole bool
	_, err = pgx.ForEachRow(rows, []any{&part, &ms, &whole}, func() error {
		out.Read = append(out.Read, part)
		if whole {
			out.Parts[part] = time.Duration(ms) * time.Millisecond
		}
		return nil
	})
	return out, err
}

// QueueSegments queues every film and episode with a part not yet asked about, in a library that
// offers markers, to be asked of the providers that time intros and credits, due as said.
func (s *Store) QueueSegments(ctx context.Context, due domain.JobDue) (int64, error) {
	return s.queueBacklog(ctx, domain.JobSegments, due, `
		INSERT INTO jobs (kind, subject, due)
		SELECT DISTINCT 'segments', i.id, $1 FROM items i
		JOIN versions v ON v.item_id = i.id AND v.missing_since IS NULL
		JOIN libraries l ON l.id = v.library_id AND l.markers <> 'off'
		JOIN parts p ON p.version_id = v.id
		WHERE i.kind IN ('movie', 'episode') AND p.segments_asked_at IS NULL
		ON CONFLICT (kind, subject) DO NOTHING`)
}
