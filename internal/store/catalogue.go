package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"golang.org/x/text/language"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
)

// Film is a title as the scanner found it in one folder.
type Film struct {
	Title  string
	Year   int
	Folder string
	IDs    map[domain.Provider]string
	NFO    *domain.Metadata
	// Artwork is the pictures of it in its folder, and Themes its theme tunes there.
	Artwork []domain.Artwork
	Themes  []string
	Copies  []Copy
}

// Copy is one version. Facts is nil for a copy whose content key is already known: nothing about
// its bytes has changed, only perhaps its paths.
type Copy struct {
	ContentKey []byte
	Edition    string
	Label      string
	Parts      []Part
	Subtitles  []Subtitle
}

// Subtitle is a subtitle file beside a copy.
type Subtitle struct {
	RelPath  string
	Size     int64
	ModTime  time.Time
	Codec    string
	Language language.Tag
	Title    string
	Forced   bool
	Default  bool
	// HearingImpaired is an SDH track: one that also describes sounds.
	HearingImpaired bool
}

type Part struct {
	RelPath string
	Size    int64
	ModTime time.Time
	Facts   *domain.Facts
}

// KnownCopies answers which of the content keys are of copies already in the library, by key as
// a string. The same file in two libraries is a copy in each.
func (s *Store) KnownCopies(ctx context.Context, lib uuid.UUID, keys [][]byte) (map[string]bool, error) {
	rows, err := s.pool.Query(ctx, `SELECT fingerprint FROM versions WHERE library_id = $1 AND fingerprint = ANY($2)`,
		lib, keys)
	if err != nil {
		return nil, err
	}
	found, err := pgx.CollectRows(rows, pgx.RowTo[[]byte])
	known := map[string]bool{}
	for _, k := range found {
		known[string(k)] = true
	}
	return known, err
}

// KnownFile is a file the last scan recorded as a part of a copy: its size and modification time
// then, and the copy's content key, its place in the copy and how many parts the copy has.
type KnownFile struct {
	Size       int64
	ModTime    time.Time
	ContentKey []byte
	Idx        int
	Parts      int
}

// KnownFiles answers the files of paths the library recorded at its last scan, by path.
func (s *Store) KnownFiles(ctx context.Context, lib uuid.UUID, paths []string) (map[string]KnownFile, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT f.rel_path, f.size_bytes, f.mtime_ns, v.fingerprint, p.idx,
			(SELECT count(*) FROM parts o WHERE o.version_id = v.id)
		FROM part_files f JOIN parts p ON p.id = f.part_id JOIN versions v ON v.id = p.version_id
		WHERE f.library_id = $1 AND f.rel_path = ANY($2)`, lib, paths)
	if err != nil {
		return nil, err
	}
	known := map[string]KnownFile{}
	_, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (struct{}, error) {
		var rel string
		var f KnownFile
		var mtime int64
		err := r.Scan(&rel, &f.Size, &mtime, &f.ContentKey, &f.Idx, &f.Parts)
		f.ModTime = time.Unix(0, mtime)
		known[rel] = f
		return struct{}{}, err
	})
	return known, err
}

// FolderFingerprints answers the fingerprint each of a library's folders had when it was last
// scanned, by path.
func (s *Store) FolderFingerprints(ctx context.Context, lib uuid.UUID) (map[string][]byte, error) {
	rows, err := s.pool.Query(ctx, `SELECT path, fingerprint FROM folders WHERE library_id = $1`, lib)
	if err != nil {
		return nil, err
	}
	known := map[string][]byte{}
	var path string
	var fingerprint []byte
	_, err = pgx.ForEachRow(rows, []any{&path, &fingerprint}, func() error {
		known[path] = fingerprint
		return nil
	})
	return known, err
}

// Changed is the titles a write added, changed and removed.
type Changed map[domain.TitleChange][]uuid.UUID

func (c Changed) add(change domain.TitleChange, ids []uuid.UUID) {
	c[change] = append(c[change], ids...)
}

// note records a title written: added if it was made, else changed.
func (c Changed) note(made bool, id uuid.UUID) {
	change := domain.TitleUpdated
	if made {
		change = domain.TitleAdded
	}
	c[change] = append(c[change], id)
}

// Saved is what a folder's save did: the paths of extras no single title owns, and the titles it
// added and changed.
type Saved struct {
	Unowned []string
	Titles  Changed
}

// SaveFolder writes a scanned folder's films and extras and remembers its fingerprint, in one
// transaction.
func (s *Store) SaveFolder(ctx context.Context, lib uuid.UUID, path string, fingerprint []byte, films []Film, extras []Extra) (Saved, error) {
	saved := Saved{Titles: Changed{}}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		settings, err := analysisOf(ctx, tx, lib)
		if err != nil {
			return err
		}
		for _, f := range films {
			if err := saveFilm(ctx, tx, lib, settings, f, saved.Titles); err != nil {
				return fmt.Errorf("%s: %w", f.Title, err)
			}
		}
		if saved.Unowned, err = saveExtras(ctx, tx, lib, settings, extras); err != nil {
			return err
		}
		return rememberFolder(ctx, tx, lib, path, fingerprint)
	})
	return saved, err
}

// analysis is what a library asks to be made of the media a scan adds to it, and when the server's
// timings have that work done.
type analysis struct {
	model.Library
	PreviewsDue, MarkersDue domain.JobDue
}

func analysisOf(ctx context.Context, tx db, lib uuid.UUID) (analysis, error) {
	var a analysis
	var previews, markers domain.Timing
	err := tx.QueryRow(ctx, `
		SELECT l.previews, l.keyframes, l.markers, s.previews_timing, s.markers_timing FROM libraries l, server s
		WHERE l.id = $1`, lib).Scan(&a.Previews, &a.Keyframes, &a.Markers, &previews, &markers)
	a.PreviewsDue, a.MarkersDue = addedDue(previews), addedDue(markers)
	return a, err
}

// rememberFolder keeps the fingerprint a folder had when it was scanned.
func rememberFolder(ctx context.Context, tx db, lib uuid.UUID, path string, fingerprint []byte) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO folders (library_id, path, fingerprint) VALUES ($1, $2, $3)
		ON CONFLICT (library_id, path) DO UPDATE SET fingerprint = excluded.fingerprint`, lib, path, fingerprint)
	return err
}

// insertItem adds a title the scanner found, filling in its id.
func insertItem(ctx context.Context, tx db, row *model.Item) error {
	return tx.QueryRow(ctx, `
		INSERT INTO items (library_id, kind, parent_id, season_number, episode_number, episode_end, air_date,
			extra_kind, scan_title, title, sort_title, folder)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12) RETURNING id`,
		row.LibraryID, row.Kind, row.ParentID, row.SeasonNumber, row.EpisodeNumber, row.EpisodeEnd, row.AirDate,
		row.ExtraKind, row.ScanTitle, row.Title, row.SortTitle, row.Folder).Scan(&row.ID)
}

func saveFilm(ctx context.Context, tx db, lib uuid.UUID, settings analysis, f Film, changed Changed) error {
	itemID, err := filmItem(ctx, tx, lib, f, changed)
	if err != nil {
		return err
	}
	if err := saveFolderArtwork(ctx, tx, itemID, f.Folder, f.Artwork); err != nil {
		return err
	}
	if err := saveFolderThemes(ctx, tx, itemID, f.Folder, f.Themes); err != nil {
		return err
	}
	for _, c := range f.Copies {
		if err := saveCopy(ctx, tx, lib, settings, itemID, c); err != nil {
			return err
		}
	}
	// The end of each copy is read for credits; the reading passes over a copy already read.
	if settings.Markers == domain.MarkersAll && len(f.Copies) > 0 {
		if err := insertJob(ctx, tx, domain.JobMarkers, itemID, 0, 0, settings.MarkersDue); err != nil {
			return err
		}
	}
	return keyTitle(ctx, tx, itemID)
}

// filmItem is the title a film's copies belong to: the title of a copy already known, else a title
// of the same name in the same folder, else a new one.
func filmItem(ctx context.Context, tx db, lib uuid.UUID, f Film, changed Changed) (uuid.UUID, error) {
	id, known, err := knownItem(ctx, tx, lib, domain.ItemMovie, f.Copies)
	if err != nil {
		return uuid.UUID{}, err
	}
	if !known {
		err := tx.QueryRow(ctx, `SELECT id FROM items WHERE library_id = $1 AND folder = $2 AND scan_title = $3 LIMIT 1`,
			lib, f.Folder, f.Title).Scan(&id)
		switch {
		case err == nil:
		case !errors.Is(err, pgx.ErrNoRows):
			return uuid.UUID{}, err
		default:
			item := model.Item{
				LibraryID: lib, Kind: domain.ItemMovie,
				ScanTitle: f.Title, Title: f.Title, SortTitle: sortTitle(f.Title), Folder: f.Folder,
			}
			if err := insertItem(ctx, tx, &item); err != nil {
				return uuid.UUID{}, err
			}
			if err := enqueue(ctx, tx, domain.JobIdentify, item.ID); err != nil {
				return uuid.UUID{}, err
			}
			changed.note(true, item.ID)
			return item.ID, describe(ctx, tx, item.ID, f.Title, f.Year, f.IDs, f.NFO)
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE items SET scan_title = $2, folder = $3 WHERE id = $1`, id, f.Title, f.Folder); err != nil {
		return uuid.UUID{}, err
	}
	changed.note(false, id)
	return id, describe(ctx, tx, id, f.Title, f.Year, f.IDs, f.NFO)
}

// knownItem is the title of kind holding the first of copies the catalogue already has. A copy
// held by a title of another kind does not count: a film's copy first met as an extra is
// reclaimed by the film, and the emptied extra goes at the end of the scan. Nor does one an admin
// split off, whose title is its alone.
func knownItem(ctx context.Context, tx db, lib uuid.UUID, kind domain.ItemKind, copies []Copy) (uuid.UUID, bool, error) {
	for _, c := range copies {
		var id uuid.UUID
		err := tx.QueryRow(ctx, `
			SELECT v.item_id FROM versions v JOIN items i ON i.id = v.item_id
			WHERE v.library_id = $1 AND v.fingerprint = $2 AND i.kind = $3 AND v.split_at IS NULL LIMIT 1`,
			lib, c.ContentKey, kind).Scan(&id)
		if err == nil {
			return id, true, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return uuid.UUID{}, false, err
		}
	}
	return uuid.UUID{}, false, nil
}

func saveCopy(ctx context.Context, tx db, lib uuid.UUID, settings analysis, itemID uuid.UUID, c Copy) error {
	edition, label := optional(c.Edition), optional(c.Label)
	var known uuid.UUID
	err := tx.QueryRow(ctx, `SELECT id FROM versions WHERE library_id = $1 AND fingerprint = $2 LIMIT 1`,
		lib, c.ContentKey).Scan(&known)
	if err == nil {
		// A copy an admin split off stays on its own title.
		_, err := tx.Exec(ctx, `
			UPDATE versions SET item_id = CASE WHEN split_at IS NULL THEN $2 ELSE item_id END, edition = $3, label = $4,
				missing_since = NULL
			WHERE id = $1`, known, itemID, edition, label)
		if err != nil {
			return err
		}
		return addPlaces(ctx, tx, lib, known, c)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}

	version := model.Version{
		ItemID: itemID, LibraryID: lib, Fingerprint: c.ContentKey, Edition: edition, Label: label,
	}
	var offset int64
	for _, part := range c.Parts {
		version.SizeBytes += part.Size
		version.DurationMS += part.Facts.Duration.Milliseconds()
	}
	first := c.Parts[0].Facts
	version.Container = first.Container
	if video := firstVideo(first); video != nil {
		version.Width, version.Height = &video.Width, &video.Height
		version.VideoCodec, version.VideoRange = &video.Codec, &video.Range
		if video.DolbyVision != nil {
			profile := int16(video.DolbyVision.Profile)
			version.DVProfile = &profile
		}
	}
	if version.DurationMS > 0 {
		version.BitrateKbps = int(version.SizeBytes * 8 / version.DurationMS)
	}
	err = tx.QueryRow(ctx, `
		INSERT INTO versions (item_id, library_id, fingerprint, edition, label, container, width, height,
			video_codec, video_range, dv_profile, bitrate_kbps, size_bytes, duration_ms)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14) RETURNING id`,
		version.ItemID, version.LibraryID, version.Fingerprint, version.Edition, version.Label, version.Container,
		version.Width, version.Height, version.VideoCodec, version.VideoRange, version.DVProfile,
		version.BitrateKbps, version.SizeBytes, version.DurationMS).Scan(&version.ID)
	if err != nil {
		return err
	}
	previews := settings.Previews != domain.PreviewsOff
	for idx, part := range c.Parts {
		row := model.Part{
			VersionID: version.ID, Idx: int16(idx), SizeBytes: part.Size,
			DurationMS: part.Facts.Duration.Milliseconds(), OffsetMS: offset,
		}
		offset += row.DurationMS
		err := tx.QueryRow(ctx, `
			INSERT INTO parts (version_id, idx, size_bytes, duration_ms, offset_ms) VALUES ($1, $2, $3, $4, $5)
			RETURNING id`, row.VersionID, row.Idx, row.SizeBytes, row.DurationMS, row.OffsetMS).Scan(&row.ID)
		if err != nil {
			return err
		}
		if err := locate(ctx, tx, lib, row.ID, part); err != nil {
			return err
		}
		if err := saveFacts(ctx, tx, row.ID, part.Facts); err != nil {
			return err
		}
		if firstVideo(part.Facts) != nil {
			if settings.Keyframes != domain.KeyframesOff {
				if err := insertJob(ctx, tx, domain.JobKeyframes, row.ID, 0, indexPriority, domain.JobDueNow); err != nil {
					return err
				}
			}
			if previews {
				if err := insertJob(ctx, tx, domain.JobPreviews, row.ID, 0, 0, settings.PreviewsDue); err != nil {
					return err
				}
			}
		}
	}
	return saveSubtitles(ctx, tx, lib, version.ID, c.Subtitles)
}

// addPlaces records where a known copy's parts and subtitles are found this time.
func addPlaces(ctx context.Context, tx db, lib, versionID uuid.UUID, c Copy) error {
	for idx, part := range c.Parts {
		var id uuid.UUID
		err := tx.QueryRow(ctx, `SELECT id FROM parts WHERE version_id = $1 AND idx = $2 LIMIT 1`, versionID, int16(idx)).Scan(&id)
		if err != nil {
			return err
		}
		if err := locate(ctx, tx, lib, id, part); err != nil {
			return err
		}
	}
	return saveSubtitles(ctx, tx, lib, versionID, c.Subtitles)
}

// saveSubtitles records a copy's subtitle files. A path already known moves to this copy, so a
// subtitle renamed onto another version follows it.
func saveSubtitles(ctx context.Context, tx db, lib, versionID uuid.UUID, subs []Subtitle) error {
	if len(subs) == 0 {
		return nil
	}
	b := &pgx.Batch{}
	for _, s := range subs {
		var lang *string
		if s.Language != language.Und {
			lang = optional(s.Language.String())
		}
		b.Queue(`
			INSERT INTO subtitle_files (version_id, library_id, rel_path, codec, language, title, forced, is_default,
				hearing_impaired, size_bytes, mtime_ns)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
			ON CONFLICT (library_id, rel_path) DO UPDATE SET version_id = excluded.version_id, codec = excluded.codec,
				language = excluded.language, title = excluded.title, forced = excluded.forced,
				is_default = excluded.is_default, hearing_impaired = excluded.hearing_impaired,
				size_bytes = excluded.size_bytes, mtime_ns = excluded.mtime_ns`,
			versionID, lib, s.RelPath, s.Codec, lang, optional(s.Title), s.Forced, s.Default, s.HearingImpaired,
			s.Size, s.ModTime.UnixNano())
	}
	return tx.SendBatch(ctx, b).Close()
}

// locate records that a part's bytes are at the part's path. A path that held other bytes before
// now holds these, so its row moves to this part, and a part it leaves nowhere was replaced: its
// previews go now, where a part left nowhere by an unmounted disk keeps them a while.
func locate(ctx context.Context, tx db, lib, partID uuid.UUID, part Part) error {
	_, err := tx.Exec(ctx, `
		DELETE FROM previews pv USING part_files f
		WHERE f.library_id = $1 AND f.rel_path = $2 AND f.part_id <> $3 AND pv.part_id = f.part_id
			AND NOT EXISTS (SELECT 1 FROM part_files o WHERE o.part_id = f.part_id
				AND (o.library_id, o.rel_path) <> (f.library_id, f.rel_path))`,
		lib, part.RelPath, partID)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO part_files (part_id, library_id, rel_path, size_bytes, mtime_ns) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (library_id, rel_path) DO UPDATE SET part_id = excluded.part_id,
			size_bytes = excluded.size_bytes, mtime_ns = excluded.mtime_ns`,
		partID, lib, part.RelPath, part.Size, part.ModTime.UnixNano())
	return err
}

func saveFacts(ctx context.Context, tx db, partID uuid.UUID, f *domain.Facts) error {
	b := &pgx.Batch{}
	for _, st := range f.Streams {
		row := model.Stream{
			PartID: partID, Idx: st.Index, Kind: st.Kind, Codec: st.Codec,
			Profile: optional(st.Profile), Title: optional(st.Title),
			IsDefault: st.Default, Forced: st.Forced, HearingImpaired: st.HearingImpaired, Commentary: st.Commentary,
			BitrateKbps: optionalInt(st.BitrateKbps),
		}
		if st.Language != language.Und {
			row.Language = optional(st.Language.String())
		}
		switch st.Kind {
		case domain.StreamVideo:
			row.Width, row.Height, row.FrameRate = optionalInt(st.Width), optionalInt(st.Height), &st.FrameRate
			row.VideoRange, row.Level, row.Interlaced = &st.Range, optionalInt(st.Level), st.Interlaced
			if st.BitDepth > 0 {
				depth := int16(st.BitDepth)
				row.BitDepth = &depth
			}
			if dv := st.DolbyVision; dv != nil {
				profile, level, compat := int16(dv.Profile), int16(dv.Level), int16(dv.Compatibility)
				row.DVProfile, row.DVLevel, row.DVCompatibility = &profile, &level, &compat
			}
		case domain.StreamAudio:
			row.Channels, row.ChannelLayout = optionalInt(st.Channels), optional(st.ChannelLayout)
			row.SampleRate = optionalInt(st.SampleRate)
		case domain.StreamSubtitle:
		}
		b.Queue(`INSERT INTO streams (`+streamColumns+`) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12,
			$13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25)`,
			row.PartID, row.Idx, row.Kind, row.Codec, row.Profile, row.Language, row.Title, row.IsDefault, row.Forced,
			row.HearingImpaired, row.Commentary, row.Width, row.Height, row.FrameRate, row.BitDepth, row.Level,
			row.VideoRange, row.Interlaced, row.DVProfile, row.DVLevel, row.DVCompatibility, row.Channels,
			row.ChannelLayout, row.SampleRate, row.BitrateKbps)
	}
	for i, c := range f.Chapters {
		b.Queue(`INSERT INTO chapters (`+chapterColumns+`) VALUES ($1, $2, $3, $4, $5)`,
			partID, i, c.Start.Milliseconds(), c.End.Milliseconds(), optional(c.Title))
	}
	if b.Len() == 0 {
		return nil
	}
	return tx.SendBatch(ctx, b).Close()
}

// FinishScan settles a library after every folder of a scan has been seen, scopes being the
// folders it read with everything under them ("." for the whole library). present holds every
// video and subtitle path the walk found, skipped folders included. A path in scope no longer
// present stops being a place to read its part, or is a subtitle no longer there; a version with a
// part left nowhere is marked missing (kept, so an unmounted disk does not cost its titles), and
// one whose every part is somewhere is not. A title left with no version is removed, then a season
// and a show left with nothing in them. A folder in scope the walk did not visit is forgotten. It
// answers the titles whose copies went missing or came back, and those removed.
func (s *Store) FinishScan(ctx context.Context, lib uuid.UUID, scopes, folders, present []string) (Changed, error) {
	var changed Changed
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		changed = Changed{}
		for _, table := range []string{"part_files", "subtitle_files"} {
			_, err := tx.Exec(ctx, `DELETE FROM `+table+` WHERE library_id = $1 AND `+notIn("rel_path", "$2")+` AND `+inScope("rel_path", "$3"),
				lib, present, scopes)
			if err != nil {
				return err
			}
		}
		// Only a version whose part went missing, or came back, is written. This and the sweeps
		// below read the whole library whatever the scopes: a save in scope can take a path, a copy,
		// an episode or an extra from a title outside them, and identifying empties collections.
		updated, err := queryIDs(ctx, tx, `
			UPDATE versions v SET missing_since = CASE WHEN v.missing_since IS NULL THEN now() END
			WHERE v.library_id = $1 AND (v.missing_since IS NULL) = EXISTS (SELECT 1 FROM parts p
				WHERE p.version_id = v.id AND NOT EXISTS (SELECT 1 FROM part_files f WHERE f.part_id = p.id))
			RETURNING v.item_id`, lib)
		if err != nil {
			return err
		}
		changed.add(domain.TitleUpdated, updated)
		for _, sql := range []string{
			`DELETE FROM items i WHERE i.library_id = $1 AND i.kind IN ('movie', 'episode', 'extra')
				AND NOT EXISTS (SELECT 1 FROM versions v WHERE v.item_id = i.id)`,
			`DELETE FROM items i WHERE i.library_id = $1 AND i.kind = 'season'
				AND NOT EXISTS (SELECT 1 FROM items c WHERE c.parent_id = i.id)`,
			`DELETE FROM items i WHERE i.library_id = $1 AND i.kind = 'show'
				AND NOT EXISTS (SELECT 1 FROM items c WHERE c.parent_id = i.id)`,
			`DELETE FROM items i USING collections c WHERE c.item_id = i.id AND i.library_id = $1
				AND c.origin NOT IN ` + madeHere + ` AND NOT EXISTS (SELECT 1 FROM collection_members m WHERE m.collection_id = c.item_id)`,
		} {
			removed, err := queryIDs(ctx, tx, sql+` RETURNING i.id`, lib)
			if err != nil {
				return err
			}
			changed.add(domain.TitleRemoved, removed)
		}
		_, err = tx.Exec(ctx, `DELETE FROM folders WHERE library_id = $1 AND `+notIn("path", "$2")+` AND `+inScope("path", "$3"),
			lib, folders, scopes)
		return err
	})
	return changed, err
}

// notIn is the condition that a path column is none of the array parameter param. Written as
// NOT x = ANY(param), a cached generic plan compares each row with every element in turn; as an
// anti-join, the planner hashes the array once.
func notIn(column, param string) string {
	return `NOT EXISTS (SELECT 1 FROM unnest(` + param + `::text[]) p WHERE p = ` + column + `)`
}

// inScope is the condition that a path column is one of the folders of the array parameter param,
// or under one.
func inScope(column, param string) string {
	return `EXISTS (SELECT 1 FROM unnest(` + param + `::text[]) s WHERE s = '.' OR ` + column + ` = s OR starts_with(` + column + `, s || '/'))`
}

// queryIDs answers the ids a statement returns.
func queryIDs(ctx context.Context, q db, sql string, args ...any) ([]uuid.UUID, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
}

func firstVideo(f *domain.Facts) *domain.Stream {
	for i := range f.Streams {
		if f.Streams[i].Kind == domain.StreamVideo {
			return &f.Streams[i]
		}
	}
	return nil
}

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func optionalInt(n int) *int {
	if n == 0 {
		return nil
	}
	return &n
}

// sortTitle orders "The Thing" under T and ignores case.
func sortTitle(title string) string {
	t := strings.ToLower(title)
	for _, article := range []string{"the ", "a ", "an "} {
		if rest, ok := strings.CutPrefix(t, article); ok && rest != "" {
			return rest
		}
	}
	return t
}
