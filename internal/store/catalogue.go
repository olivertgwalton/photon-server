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
	"gorm.io/gen/field"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/media"
	"github.com/olivertgwalton/photon-server/internal/store/model"
	"github.com/olivertgwalton/photon-server/internal/store/query"
)

// Film is a title as the scanner found it in one folder.
type Film struct {
	Title  string
	Year   int
	Folder string
	IDs    map[domain.Provider]string
	NFO    *domain.Metadata
	// Artwork is the pictures of it in its folder.
	Artwork []domain.Artwork
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
	Facts   *media.Facts
}

// KnownCopy reports whether a copy with this content key is already in the library. The same file
// in two libraries is a copy in each.
func (s *Store) KnownCopy(ctx context.Context, lib uuid.UUID, key []byte) (bool, error) {
	v := s.q.Version
	n, err := v.WithContext(ctx).Where(v.LibraryID.Eq(model.UUID(lib)), v.Fingerprint.Eq(key)).Count()
	return n > 0, err
}

// FolderFingerprint is the fingerprint the folder had when it was last scanned.
func (s *Store) FolderFingerprint(ctx context.Context, lib uuid.UUID, path string) ([]byte, error) {
	f := s.q.Folder
	row, err := f.WithContext(ctx).Where(f.LibraryID.Eq(model.UUID(lib)), f.Path.Eq(path)).Take()
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return row.Fingerprint, nil
}

// SaveFolder writes a scanned folder's films and extras and remembers its fingerprint, in one
// transaction. It answers the paths of extras no single title owns.
func (s *Store) SaveFolder(ctx context.Context, lib uuid.UUID, path string, fingerprint []byte, films []Film, extras []Extra) ([]string, error) {
	var unowned []string
	err := s.q.Transaction(func(tx *query.Query) error {
		for _, f := range films {
			if err := saveFilm(ctx, tx, lib, f); err != nil {
				return fmt.Errorf("%s: %w", f.Title, err)
			}
		}
		var err error
		if unowned, err = saveExtras(ctx, tx, lib, extras); err != nil {
			return err
		}
		return tx.Folder.WithContext(ctx).Save(&model.Folder{
			LibraryID: model.UUID(lib), Path: path, Fingerprint: fingerprint,
		})
	})
	return unowned, err
}

func saveFilm(ctx context.Context, tx *query.Query, lib uuid.UUID, f Film) error {
	itemID, err := filmItem(ctx, tx, lib, f)
	if err != nil {
		return err
	}
	if err := saveFolderArtwork(ctx, tx, itemID, f.Folder, f.Artwork); err != nil {
		return err
	}
	for _, c := range f.Copies {
		if err := saveCopy(ctx, tx, lib, itemID, c); err != nil {
			return err
		}
	}
	return nil
}

// filmItem is the title a film's copies belong to: the title of a copy already known, else a title
// of the same name in the same folder, else a new one.
func filmItem(ctx context.Context, tx *query.Query, lib uuid.UUID, f Film) (model.UUID, error) {
	item := model.Item{
		LibraryID: model.UUID(lib), Kind: domain.ItemMovie,
		ScanTitle: f.Title, Title: f.Title, SortTitle: sortTitle(f.Title), Folder: f.Folder,
	}
	i := tx.Item
	id, known, err := knownItem(ctx, tx, lib, domain.ItemMovie, f.Copies)
	if err != nil {
		return model.UUID{}, err
	}
	item.ID = id
	if !known {
		same, err := i.WithContext(ctx).Where(
			i.LibraryID.Eq(model.UUID(lib)), i.Folder.Eq(f.Folder), i.ScanTitle.Eq(f.Title),
		).Take()
		switch {
		case err == nil:
			item.ID = same.ID
		case !errors.Is(err, gorm.ErrRecordNotFound):
			return model.UUID{}, err
		default:
			if err := i.WithContext(ctx).Create(&item); err != nil {
				return model.UUID{}, err
			}
			if err := enqueue(ctx, tx, domain.JobIdentify, item.ID); err != nil {
				return model.UUID{}, err
			}
			return item.ID, describe(ctx, tx, item.ID, f.Title, f.Year, f.IDs, f.NFO)
		}
	}
	_, err = i.WithContext(ctx).Where(i.ID.Eq(item.ID)).Select(i.ScanTitle, i.Folder).Updates(&item)
	if err != nil {
		return model.UUID{}, err
	}
	return item.ID, describe(ctx, tx, item.ID, f.Title, f.Year, f.IDs, f.NFO)
}

// knownItem is the title of kind holding the first of copies the catalogue already has. A copy
// held by a title of another kind does not count: a film's copy first met as an extra is
// reclaimed by the film, and the emptied extra goes at the end of the scan.
func knownItem(ctx context.Context, tx *query.Query, lib uuid.UUID, kind domain.ItemKind, copies []Copy) (model.UUID, bool, error) {
	v, i := tx.Version, tx.Item
	for _, c := range copies {
		var row struct{ ItemID model.UUID }
		err := v.WithContext(ctx).Select(v.ItemID).Join(i, i.ID.EqCol(v.ItemID)).
			Where(v.LibraryID.Eq(model.UUID(lib)), v.Fingerprint.Eq(c.ContentKey), i.Kind.Eq(string(kind))).Scan(&row)
		if err != nil {
			return model.UUID{}, false, err
		}
		if row.ItemID != (model.UUID{}) {
			return row.ItemID, true, nil
		}
	}
	return model.UUID{}, false, nil
}

func saveCopy(ctx context.Context, tx *query.Query, lib uuid.UUID, itemID model.UUID, c Copy) error {
	v, p := tx.Version, tx.Part
	edition, label := optional(c.Edition), optional(c.Label)
	known, err := v.WithContext(ctx).Where(v.LibraryID.Eq(model.UUID(lib)), v.Fingerprint.Eq(c.ContentKey)).Take()
	if err == nil {
		_, err := v.WithContext(ctx).Where(v.ID.Eq(known.ID)).UpdateSimple(
			v.ItemID.Value(itemID), nullable(v.Edition, c.Edition), nullable(v.Label, c.Label), v.MissingSince.Null())
		if err != nil {
			return err
		}
		return addPlaces(ctx, tx, lib, known.ID, c)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}

	version := model.Version{
		ItemID: itemID, LibraryID: model.UUID(lib), Fingerprint: c.ContentKey, Edition: edition, Label: label,
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
	if err := v.WithContext(ctx).Create(&version); err != nil {
		return err
	}
	for idx, part := range c.Parts {
		row := model.Part{
			VersionID: version.ID, Idx: int16(idx), SizeBytes: part.Size,
			DurationMS: part.Facts.Duration.Milliseconds(), OffsetMS: offset,
		}
		offset += row.DurationMS
		if err := p.WithContext(ctx).Create(&row); err != nil {
			return err
		}
		if err := locate(ctx, tx, lib, row.ID, part); err != nil {
			return err
		}
		if err := saveFacts(ctx, tx, row.ID, part.Facts); err != nil {
			return err
		}
		if firstVideo(part.Facts) != nil {
			if err := enqueue(ctx, tx, domain.JobKeyframes, row.ID); err != nil {
				return err
			}
		}
	}
	return saveSubtitles(ctx, tx, lib, version.ID, c.Subtitles)
}

// addPlaces records where a known copy's parts and subtitles are found this time.
func addPlaces(ctx context.Context, tx *query.Query, lib uuid.UUID, versionID model.UUID, c Copy) error {
	p := tx.Part
	for idx, part := range c.Parts {
		row, err := p.WithContext(ctx).Where(p.VersionID.Eq(versionID), p.Idx.Eq(int16(idx))).Take()
		if err != nil {
			return err
		}
		if err := locate(ctx, tx, lib, row.ID, part); err != nil {
			return err
		}
	}
	return saveSubtitles(ctx, tx, lib, versionID, c.Subtitles)
}

// saveSubtitles records a copy's subtitle files. A path already known moves to this copy, so a
// subtitle renamed onto another version follows it.
func saveSubtitles(ctx context.Context, tx *query.Query, lib uuid.UUID, versionID model.UUID, subs []Subtitle) error {
	if len(subs) == 0 {
		return nil
	}
	rows := make([]*model.SubtitleFile, len(subs))
	for i, s := range subs {
		rows[i] = &model.SubtitleFile{
			VersionID: versionID, LibraryID: model.UUID(lib), RelPath: s.RelPath, Codec: s.Codec,
			Title: optional(s.Title), Forced: s.Forced, IsDefault: s.Default, HearingImpaired: s.HearingImpaired,
			SizeBytes: s.Size, MtimeNS: s.ModTime.UnixNano(),
		}
		if s.Language != language.Und {
			rows[i].Language = optional(s.Language.String())
		}
	}
	return tx.SubtitleFile.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "library_id"}, {Name: "rel_path"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"version_id", "codec", "language", "title", "forced",
			"is_default", "hearing_impaired", "size_bytes", "mtime_ns",
		}),
	}).Create(rows...)
}

// locate records that a part's bytes are at the part's path. A path that held other bytes before
// now holds these, so its row moves to this part.
func locate(ctx context.Context, tx *query.Query, lib uuid.UUID, partID model.UUID, part Part) error {
	f := tx.PartFile
	return f.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "library_id"}, {Name: "rel_path"}},
		DoUpdates: clause.AssignmentColumns([]string{"part_id", "size_bytes", "mtime_ns"}),
	}).Create(&model.PartFile{
		PartID: partID, LibraryID: model.UUID(lib), RelPath: part.RelPath,
		SizeBytes: part.Size, MtimeNS: part.ModTime.UnixNano(),
	})
}

func saveFacts(ctx context.Context, tx *query.Query, partID model.UUID, f *media.Facts) error {
	streams := make([]*model.Stream, 0, len(f.Streams))
	for _, st := range f.Streams {
		row := &model.Stream{
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
			row.VideoRange, row.Level = &st.Range, optionalInt(st.Level)
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
		streams = append(streams, row)
	}
	if len(streams) > 0 {
		if err := tx.Stream.WithContext(ctx).Create(streams...); err != nil {
			return err
		}
	}
	chapters := make([]*model.Chapter, 0, len(f.Chapters))
	for i, c := range f.Chapters {
		chapters = append(chapters, &model.Chapter{
			PartID: partID, Idx: i, StartMS: c.Start.Milliseconds(), EndMS: c.End.Milliseconds(), Title: optional(c.Title),
		})
	}
	if len(chapters) > 0 {
		return tx.Chapter.WithContext(ctx).Create(chapters...)
	}
	return nil
}

// FinishScan settles a library after every folder has been seen. present holds every video and
// subtitle path the walk found, skipped folders included. A path no longer present stops being a
// place to read its part, or is a subtitle no longer there; a version with a part left nowhere is marked missing (kept, so an unmounted disk does
// not cost its titles), and one whose every part is somewhere is not. A title left with no
// version is removed, then a season and a show left with nothing in them. A folder the walk did
// not visit is forgotten.
func (s *Store) FinishScan(ctx context.Context, lib uuid.UUID, folders, present []string) error {
	// pgx sends each list as one text[] parameter; GORM would expand it into a parameter per path.
	// A nil slice would go as NULL, and NOT x = ANY(NULL) matches nothing, so an emptied library
	// would keep every path it ever had.
	if folders == nil {
		folders = []string{}
	}
	if present == nil {
		present = []string{}
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		for _, table := range []string{"part_files", "subtitle_files"} {
			_, err := tx.Exec(ctx, `DELETE FROM `+table+` WHERE library_id = $1 AND NOT rel_path = ANY($2)`,
				lib.String(), present)
			if err != nil {
				return err
			}
		}
		_, err := tx.Exec(ctx, `
			UPDATE versions v SET missing_since = CASE
				WHEN EXISTS (SELECT 1 FROM parts p WHERE p.version_id = v.id
					AND NOT EXISTS (SELECT 1 FROM part_files f WHERE f.part_id = p.id)) THEN coalesce(v.missing_since, now())
				ELSE NULL END
			WHERE v.library_id = $1`, lib.String())
		if err != nil {
			return err
		}
		for _, sql := range []string{
			`DELETE FROM items i WHERE i.library_id = $1 AND i.kind IN ('movie', 'episode', 'extra')
				AND NOT EXISTS (SELECT 1 FROM versions v WHERE v.item_id = i.id)`,
			`DELETE FROM items i WHERE i.library_id = $1 AND i.kind = 'season'
				AND NOT EXISTS (SELECT 1 FROM items c WHERE c.parent_id = i.id)`,
			`DELETE FROM items i WHERE i.library_id = $1 AND i.kind = 'show'
				AND NOT EXISTS (SELECT 1 FROM items c WHERE c.parent_id = i.id)`,
		} {
			if _, err := tx.Exec(ctx, sql, lib.String()); err != nil {
				return err
			}
		}
		_, err = tx.Exec(ctx, `DELETE FROM folders WHERE library_id = $1 AND NOT path = ANY($2)`, lib.String(), folders)
		return err
	})
}

func firstVideo(f *media.Facts) *media.Stream {
	for i := range f.Streams {
		if f.Streams[i].Kind == domain.StreamVideo {
			return &f.Streams[i]
		}
	}
	return nil
}

func nullable(f field.String, s string) field.AssignExpr {
	if s == "" {
		return f.Null()
	}
	return f.Value(s)
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
