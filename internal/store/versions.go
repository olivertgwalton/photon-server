package store

import (
	"cmp"
	"context"
	"fmt"
	"path"
	"slices"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"golang.org/x/sync/errgroup"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
)

// VersionPage is one copy: what it is, the tracks in it and the subtitles beside it. A copy whose
// files are gone says since when.
type VersionPage struct {
	ID           uuid.UUID
	Edition      string
	Label        string
	Container    string
	DurationMS   int64
	SizeBytes    int64
	BitrateKbps  int
	Parts        int
	MissingSince *time.Time
	// Streams are the first part's; the parts of one copy are cut from one master.
	Streams   []domain.Stream
	Subtitles []SubtitleRef
	Chapters  []ChapterRef
	Markers   []MarkerRef
	// Files are its parts in order, each where it starts on the copy's timeline, by the id the
	// /api/v1/parts/{id} routes take.
	Files []PartRef
	// Trickplay is the thumbnail sheets of each part that has them; a part's sheets are at
	// /api/v1/parts/{id}/trickplay/{sheet}.
	Trickplay []PartTrickplay
	// DefaultAudioStream and DefaultSubtitleStream or DefaultSubtitleFile are the tracks it plays
	// with unasked, for the profile asking: none where no subtitle comes on.
	DefaultAudioStream    *int
	DefaultSubtitleStream *int
	DefaultSubtitleFile   *uuid.UUID
}

// PartRef is one file of a copy.
type PartRef struct {
	ID uuid.UUID
	// File is the name of the part's file, without the folders it is in.
	File       string
	Index      int
	SizeBytes  int64
	DurationMS int64
	OffsetMS   int64
}

// PartTrickplay is a part's thumbnail sheets, its thumbnails timed from OffsetMS on the copy's
// timeline.
type PartTrickplay struct {
	PartID   uuid.UUID
	OffsetMS int64
	Trickplay
}

type SubtitleRef struct {
	ID              uuid.UUID
	Codec           string
	Language        string
	Title           string
	Default         bool
	Forced          bool
	HearingImpaired bool
}

// ChapterRef is a chapter on the copy's whole timeline, across its parts: the Idx'th of Part's own.
// Image is the address of its picture, for those that have one.
type ChapterRef struct {
	Part    uuid.UUID
	Idx     int
	StartMS int64
	EndMS   int64
	Title   string
	Image   string
}

// Versions answers the copies of each of items, those on disk first, then in the order Playable
// chooses one to play: the longest first; a title with none is left out. However many titles, it is the same few queries.
func (s *Store) Versions(ctx context.Context, items []uuid.UUID) (map[uuid.UUID][]VersionPage, error) {
	rows, err := queryRows[model.Version](ctx, s.pool, `SELECT `+versionColumns+` FROM versions WHERE item_id = ANY($1)`, items)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	slices.SortStableFunc(rows, func(a, b *model.Version) int {
		if (a.MissingSince == nil) != (b.MissingSince == nil) {
			return map[bool]int{true: -1, false: 1}[a.MissingSince == nil]
		}
		return cmp.Or(cmp.Compare(b.DurationMS, a.DurationMS), a.ID.Compare(b.ID))
	})
	in, err := s.versionsIn(ctx, rows)
	if err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID][]VersionPage, len(items))
	for _, r := range rows {
		out[r.ItemID] = append(out[r.ItemID], in.page(r))
	}
	return out, nil
}

// versionRows is what is in a set of copies: their parts, and the tracks, chapters, markers and
// pictures of those, and the subtitles beside them.
type versionRows struct {
	byVersion map[uuid.UUID][]*model.Part
	streams   []*model.Stream
	chapters  []*model.Chapter
	markers   []*model.Marker
	subs      []*model.SubtitleFile
	// files is the name of each part's first file.
	files     map[uuid.UUID]string
	pictured  map[uuid.UUID][]int
	sheets    map[uuid.UUID]Trickplay
	detection map[uuid.UUID]domain.MarkerDetection
}

func (s *Store) versionsIn(ctx context.Context, versions []*model.Version) (versionRows, error) {
	vids := make([]uuid.UUID, len(versions))
	for n, r := range versions {
		vids[n] = r.ID
	}
	in := versionRows{byVersion: map[uuid.UUID][]*model.Part{}}
	// Each read waits only on those it needs, so the copies cost two round trips rather than eight.
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() (err error) {
		in.subs, err = queryRows[model.SubtitleFile](gctx, s.pool, `
			SELECT `+subtitleFileColumns+` FROM subtitle_files WHERE version_id = ANY($1) ORDER BY rel_path`, vids)
		return err
	})
	g.Go(func() (err error) {
		in.detection, err = s.markerDetection(gctx, versions)
		return err
	})
	g.Go(func() error {
		parts, err := queryRows[model.Part](gctx, s.pool, `
			SELECT `+partColumns+` FROM parts WHERE version_id = ANY($1) ORDER BY version_id, idx`, vids)
		if err != nil {
			return err
		}
		var pids []uuid.UUID
		for _, p := range parts {
			in.byVersion[p.VersionID] = append(in.byVersion[p.VersionID], p)
			pids = append(pids, p.ID)
		}
		of, pctx := errgroup.WithContext(gctx)
		of.Go(func() (err error) {
			in.streams, err = queryRows[model.Stream](pctx, s.pool, `
				SELECT `+streamColumns+` FROM streams WHERE part_id = ANY($1) ORDER BY part_id, idx`, pids)
			return err
		})
		of.Go(func() (err error) {
			in.chapters, err = queryRows[model.Chapter](pctx, s.pool, `
				SELECT `+chapterColumns+` FROM chapters WHERE part_id = ANY($1) ORDER BY part_id, idx`, pids)
			return err
		})
		of.Go(func() (err error) {
			in.markers, err = queryRows[model.Marker](pctx, s.pool, `SELECT `+markerColumns+` FROM markers WHERE part_id = ANY($1)`, pids)
			return err
		})
		of.Go(func() (err error) {
			in.files, err = s.partFiles(pctx, pids)
			return err
		})
		of.Go(func() (err error) {
			in.pictured, in.sheets, err = s.partPreviews(pctx, parts)
			return err
		})
		return of.Wait()
	})
	return in, g.Wait()
}

// partFiles answers the name of each part's first file, without the folders it is in.
func (s *Store) partFiles(ctx context.Context, parts []uuid.UUID) (map[uuid.UUID]string, error) {
	files := map[uuid.UUID]string{}
	named, err := s.pool.Query(ctx, `
		SELECT DISTINCT ON (part_id) part_id, rel_path FROM part_files WHERE part_id = ANY($1) ORDER BY part_id, rel_path`, parts)
	if err != nil {
		return nil, err
	}
	var part uuid.UUID
	var rel string
	_, err = pgx.ForEachRow(named, []any{&part, &rel}, func() error {
		files[part] = path.Base(rel)
		return nil
	})
	return files, err
}

// page is the copy r, from what is in it.
func (in versionRows) page(r *model.Version) VersionPage {
	vp := VersionPage{
		ID: r.ID, Edition: deref(r.Edition), Label: deref(r.Label), Container: r.Container,
		DurationMS: r.DurationMS, SizeBytes: r.SizeBytes, BitrateKbps: r.BitrateKbps,
		Parts: len(in.byVersion[r.ID]), MissingSince: r.MissingSince, Streams: []domain.Stream{},
	}
	for k, p := range in.byVersion[r.ID] {
		vp.Files = append(vp.Files, PartRef{
			ID: p.ID, File: in.files[p.ID], Index: int(p.Idx), SizeBytes: p.SizeBytes, DurationMS: p.DurationMS, OffsetMS: p.OffsetMS,
		})
		var own []*model.Chapter
		for _, c := range in.chapters {
			if c.PartID == p.ID {
				own = append(own, c)
				ref := ChapterRef{Part: p.ID, Idx: c.Idx, StartMS: p.OffsetMS + c.StartMS, EndMS: p.OffsetMS + c.EndMS, Title: deref(c.Title)}
				if slices.Contains(in.pictured[p.ID], c.Idx) {
					ref.Image = fmt.Sprintf("/api/v1/parts/%s/chapters/%d/image", p.ID, c.Idx)
				}
				vp.Chapters = append(vp.Chapters, ref)
			}
		}
		var stored []*model.Marker
		for _, m := range in.markers {
			if m.PartID == p.ID {
				stored = append(stored, m)
			}
		}
		for _, m := range partMarkers(stored, own, in.detection[r.LibraryID]) {
			m.StartMS += p.OffsetMS
			m.EndMS += p.OffsetMS
			vp.Markers = append(vp.Markers, m)
		}
		if t, ok := in.sheets[p.ID]; ok {
			vp.Trickplay = append(vp.Trickplay, PartTrickplay{PartID: p.ID, OffsetMS: p.OffsetMS, Trickplay: t})
		}
		if k > 0 {
			continue
		}
		for _, t := range in.streams {
			if t.PartID == p.ID {
				vp.Streams = append(vp.Streams, mediaStream(t))
			}
		}
	}
	for _, f := range in.subs {
		if f.VersionID == r.ID {
			vp.Subtitles = append(vp.Subtitles, SubtitleRef{
				ID: f.ID, Codec: f.Codec, Language: deref(f.Language), Title: deref(f.Title), Default: f.IsDefault,
				Forced: f.Forced, HearingImpaired: f.HearingImpaired,
			})
		}
	}
	return vp
}

// markerDetection answers how each copy's library finds markers.
func (s *Store) markerDetection(ctx context.Context, versions []*model.Version) (map[uuid.UUID]domain.MarkerDetection, error) {
	libs := make([]uuid.UUID, len(versions))
	for n, v := range versions {
		libs[n] = v.LibraryID
	}
	return queryMap[uuid.UUID, domain.MarkerDetection](ctx, s.pool,
		`SELECT id, markers FROM libraries WHERE id = ANY($1)`, libs)
}

// partPreviews answers the idx of each part's chapters that have an image, and each part's
// trickplay sheets.
func (s *Store) partPreviews(ctx context.Context, parts []*model.Part) (map[uuid.UUID][]int, map[uuid.UUID]Trickplay, error) {
	ids := make([]uuid.UUID, len(parts))
	for n, p := range parts {
		ids[n] = p.ID
	}
	rows, err := s.pool.Query(ctx, `
		SELECT pv.part_id, pv.chapter_images, t.width, t.height, t.interval_ms, t.columns, t.rows, t.thumbnails
		FROM previews pv LEFT JOIN trickplay t ON t.part_id = pv.part_id WHERE pv.part_id = ANY($1)`, ids)
	if err != nil {
		return nil, nil, err
	}
	pictured, sheets := map[uuid.UUID][]int{}, map[uuid.UUID]Trickplay{}
	var part uuid.UUID
	var idx []int
	var w, h, interval, cols, rws, n *int
	_, err = pgx.ForEachRow(rows, []any{&part, &idx, &w, &h, &interval, &cols, &rws, &n}, func() error {
		pictured[part] = idx
		if n != nil {
			sheets[part] = sheetsOf(*w, *h, *interval, *cols, *rws, *n)
		}
		return nil
	})
	return pictured, sheets, err
}
