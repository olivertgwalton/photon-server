package store

import (
	"cmp"
	"context"
	"errors"
	"maps"
	"slices"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
)

var (
	ErrMarkerOutsidePart = errors.New("a marker must lie within one part of the copy")
	ErrMarkerRepeated    = errors.New("a part has one marker of each kind")
	ErrMarkerNoPart      = errors.New("the copy has no part of that number")
)

// markersQuiet is how long a season's sound waits after its last episode arrives before it is
// compared, so a season scanned folder by folder is compared once.
const markersQuiet = 10 * time.Minute

// MarkerRef is a stretch a player may offer to skip, on the copy's whole timeline like a chapter.
type MarkerRef struct {
	Kind    domain.MarkerKind
	StartMS int64
	EndMS   int64
	Source  domain.MarkerSource
}

// partMarkers is one marker of each kind a part has, the most trusted source's its library
// offers: what an admin said, then the part's chapters, then its season's fingerprints. An admin's
// word that there is none outranks the others' stretches the same way. Fingerprints found before a
// library stopped comparing are kept, and offered again should it start.
func partMarkers(stored []*model.Marker, chapters []*model.Chapter, detection domain.MarkerDetection) []MarkerRef {
	rank := func(s domain.MarkerSource) int { return slices.Index(domain.MarkerSources(), s) }
	byKind := map[domain.MarkerKind]*model.Marker{}
	for _, m := range slices.Concat(stored, chapterMarkers(chapters)) {
		if !detection.Keeps(m.Source) {
			continue
		}
		if have, ok := byKind[m.Kind]; !ok || rank(m.Source) < rank(have.Source) {
			byKind[m.Kind] = m
		}
	}
	out := make([]MarkerRef, 0, len(byKind))
	for _, m := range byKind {
		if m.StartMS != nil {
			out = append(out, MarkerRef{Kind: m.Kind, StartMS: *m.StartMS, EndMS: *m.EndMS, Source: m.Source})
		}
	}
	slices.SortFunc(out, func(a, b MarkerRef) int { return cmp.Compare(a.StartMS, b.StartMS) })
	return out
}

// chapterMarkers are the stretches a part's chapters name, as Intro Skipper reads them: the first
// intro and recap, the last credits and preview, each of a plausible length.
func chapterMarkers(chapters []*model.Chapter) []*model.Marker {
	found := map[domain.MarkerKind]*model.Marker{}
	for _, c := range chapters {
		kind, ok := domain.ChapterMarker(deref(c.Title))
		length := time.Duration(c.EndMS-c.StartMS) * time.Millisecond
		if !ok || length < domain.MarkerShortest || length > kind.Longest() {
			continue
		}
		if _, seen := found[kind]; seen && (kind == domain.MarkerIntro || kind == domain.MarkerRecap) {
			continue
		}
		found[kind] = &model.Marker{Kind: kind, Source: domain.MarkerByChapter, StartMS: &c.StartMS, EndMS: &c.EndMS}
	}
	return slices.Collect(maps.Values(found))
}

// SetMarkers replaces what an admin says of a copy's markers: stretches given on its whole
// timeline, and parts that have none of a kind. Saying nothing clears them, and the chapters' and
// fingerprints' stand again.
func (s *Store) SetMarkers(ctx context.Context, version uuid.UUID, markers []domain.Marker, absent []domain.MarkerAbsent) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		parts, err := queryRows[model.Part](ctx, tx, `SELECT `+partColumns+` FROM parts WHERE version_id = $1 ORDER BY idx`, version)
		if err != nil {
			return err
		}
		if len(parts) == 0 {
			return ErrNotFound
		}
		rows := make([]*model.Marker, 0, len(markers)+len(absent))
		add := func(m *model.Marker) error {
			if slices.ContainsFunc(rows, func(r *model.Marker) bool { return r.PartID == m.PartID && r.Kind == m.Kind }) {
				return ErrMarkerRepeated
			}
			rows = append(rows, m)
			return nil
		}
		for _, m := range markers {
			i := slices.IndexFunc(parts, func(p *model.Part) bool {
				return p.OffsetMS <= m.StartMS && m.EndMS <= p.OffsetMS+p.DurationMS
			})
			if i < 0 {
				return ErrMarkerOutsidePart
			}
			p := parts[i]
			start, end := m.StartMS-p.OffsetMS, m.EndMS-p.OffsetMS
			if err := add(&model.Marker{PartID: p.ID, Kind: m.Kind, Source: domain.MarkerByUser, StartMS: &start, EndMS: &end}); err != nil {
				return err
			}
		}
		for _, a := range absent {
			if a.Part >= len(parts) {
				return ErrMarkerNoPart
			}
			if err := add(&model.Marker{PartID: parts[a.Part].ID, Kind: a.Kind, Source: domain.MarkerByUser}); err != nil {
				return err
			}
		}
		pids := make([]uuid.UUID, len(parts))
		for n, p := range parts {
			pids[n] = p.ID
		}
		_, err = tx.Exec(ctx, `DELETE FROM markers WHERE part_id = ANY($1) AND source = $2`, pids, domain.MarkerByUser)
		if err != nil {
			return err
		}
		return createMarkers(ctx, tx, rows)
	})
}

func createMarkers(ctx context.Context, tx db, rows []*model.Marker) error {
	if len(rows) == 0 {
		return nil
	}
	b := &pgx.Batch{}
	for _, m := range rows {
		b.Queue(`INSERT INTO markers (part_id, kind, source, start_ms, end_ms) VALUES ($1, $2, $3, $4, $5)`,
			m.PartID, m.Kind, m.Source, m.StartMS, m.EndMS)
	}
	return tx.SendBatch(ctx, b).Close()
}

// SeasonPart is a part of an episode in a season, with somewhere to read it and sound to compare.
type SeasonPart struct {
	ID      uuid.UUID
	Episode uuid.UUID
	Version uuid.UUID
	Idx     int
	// Duration is the part's own length.
	Duration time.Duration
	Root     string
	RelPath  string
	// Fingerprinted is whether its sound has been compared with its season's.
	Fingerprinted bool
}

// SeasonParts answers the parts of a season's episodes that are on disk, in a library that compares
// sound, and have sound, by episode, copy and order.
func (s *Store) SeasonParts(ctx context.Context, season uuid.UUID) ([]SeasonPart, error) {
	return queryStructs[SeasonPart](ctx, s.pool, `
		SELECT * FROM (
			SELECT DISTINCT ON (p.id) p.id, e.id AS episode, v.id AS version, p.idx,
				p.duration_ms * interval '1 millisecond' AS duration, l.root, f.rel_path,
				p.fingerprinted_at IS NOT NULL AS fingerprinted
			FROM items e
			JOIN versions v ON v.item_id = e.id AND v.missing_since IS NULL
			JOIN parts p ON p.version_id = v.id
			JOIN part_files f ON f.part_id = p.id
			JOIN libraries l ON l.id = f.library_id AND l.markers = 'all'
			WHERE e.parent_id = $1 AND e.kind = 'episode'
				AND EXISTS (SELECT 1 FROM streams s WHERE s.part_id = p.id AND s.kind = 'audio')
			ORDER BY p.id, f.rel_path
		) found
		ORDER BY episode, version, idx`, season)
}

// FilmEnd is the last part of a copy of a film, where its credits are.
type FilmEnd struct {
	ID            uuid.UUID
	Duration      time.Duration
	Root, RelPath string
	Read          bool
}

// FilmEnds answers the last part of each copy of a film on disk, in a library that reads its files
// for markers, with a picture: none for anything but a film.
func (s *Store) FilmEnds(ctx context.Context, film uuid.UUID) ([]FilmEnd, error) {
	return queryStructs[FilmEnd](ctx, s.pool, `
		SELECT DISTINCT ON (p.id) p.id, p.duration_ms * interval '1 millisecond' AS duration, l.root, f.rel_path,
			p.fingerprinted_at IS NOT NULL AS read
		FROM items i
		JOIN versions v ON v.item_id = i.id AND v.missing_since IS NULL
		JOIN parts p ON p.version_id = v.id AND p.idx = (SELECT max(idx) FROM parts WHERE version_id = v.id)
		JOIN part_files f ON f.part_id = p.id
		JOIN libraries l ON l.id = f.library_id AND l.markers = 'all'
		WHERE i.id = $1 AND i.kind = 'movie'
			AND EXISTS (SELECT 1 FROM streams s WHERE s.part_id = p.id AND s.kind = 'video')
		ORDER BY p.id, f.rel_path`, film)
}

// SaveFoundMarkers replaces the markers source found on the parts read, and records that they were.
func (s *Store) SaveFoundMarkers(ctx context.Context, source domain.MarkerSource, read []uuid.UUID, found map[uuid.UUID][]domain.Marker) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `DELETE FROM markers WHERE part_id = ANY($1) AND source = $2`, read, source)
		if err != nil {
			return err
		}
		var rows []*model.Marker
		for part, markers := range found {
			for _, m := range markers {
				rows = append(rows, &model.Marker{
					PartID: part, Kind: m.Kind, Source: source, StartMS: &m.StartMS, EndMS: &m.EndMS,
				})
			}
		}
		if err := createMarkers(ctx, tx, rows); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE parts SET fingerprinted_at = $2 WHERE id = ANY($1)`, read, time.Now())
		return err
	})
}

// QueueSeasonMarkers queues a comparison of every season with an episode whose sound has not been
// compared, in a library that compares sound, due as said; due now, every markers job already
// queued is due now too. A season whose comparison failed every attempt waits for its episodes to
// change.
func (s *Store) QueueSeasonMarkers(ctx context.Context, due domain.JobDue) (int64, error) {
	return s.queueBacklog(ctx, domain.JobMarkers, due, `
		INSERT INTO jobs (kind, subject, due)
		SELECT DISTINCT 'markers', e.parent_id, $1 FROM items e
		JOIN versions v ON v.item_id = e.id AND v.missing_since IS NULL
		JOIN libraries l ON l.id = v.library_id AND l.markers = 'all'
		JOIN parts p ON p.version_id = v.id
		WHERE e.kind = 'episode' AND p.fingerprinted_at IS NULL
			AND EXISTS (SELECT 1 FROM streams s WHERE s.part_id = p.id AND s.kind = 'audio')
		ON CONFLICT (kind, subject) DO NOTHING`)
}

// QueueFilmMarkers queues a reading of the end of every film whose last part has not been read, in
// a library that reads its files for markers, due as said, as QueueSeasonMarkers queues seasons.
func (s *Store) QueueFilmMarkers(ctx context.Context, due domain.JobDue) (int64, error) {
	return s.queueBacklog(ctx, domain.JobMarkers, due, `
		INSERT INTO jobs (kind, subject, due)
		SELECT DISTINCT 'markers', i.id, $1 FROM items i
		JOIN versions v ON v.item_id = i.id AND v.missing_since IS NULL
		JOIN libraries l ON l.id = v.library_id AND l.markers = 'all'
		JOIN parts p ON p.version_id = v.id AND p.idx = (SELECT max(idx) FROM parts WHERE version_id = v.id)
		WHERE i.kind = 'movie' AND p.fingerprinted_at IS NULL
			AND EXISTS (SELECT 1 FROM streams s WHERE s.part_id = p.id AND s.kind = 'video')
		ON CONFLICT (kind, subject) DO NOTHING`)
}
