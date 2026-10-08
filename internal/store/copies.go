package store

import (
	"context"
	"errors"
	"strings"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"golang.org/x/text/language"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
)

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

func saveCopy(ctx context.Context, tx db, lib uuid.UUID, settings analysis, itemID uuid.UUID, c Copy) error {
	edition, label := optional(c.Edition), optional(c.Label)
	var known uuid.UUID
	var anotherEpisodes bool
	err := tx.QueryRow(ctx, `
		SELECT v.id, v.item_id <> $3 AND v.split_at IS NULL AND i.kind = 'episode' AND (SELECT kind FROM items WHERE id = $3) = 'episode'
		FROM versions v JOIN items i ON i.id = v.item_id
		WHERE v.library_id = $1 AND v.fingerprint = $2 ORDER BY v.item_id = $3 DESC LIMIT 1`,
		lib, c.ContentKey, itemID).Scan(&known, &anotherEpisodes)
	if err == nil && anotherEpisodes {
		return copyVersion(ctx, tx, lib, settings, known, itemID, c)
	}
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
			if err := indexVideo(ctx, tx, settings, row.ID); err != nil {
				return err
			}
		}
	}
	return saveSubtitles(ctx, tx, lib, version.ID, c.Subtitles)
}

// indexVideo queues what a library asks to be made of a new part holding video.
func indexVideo(ctx context.Context, tx db, settings analysis, part uuid.UUID) error {
	if settings.Keyframes != domain.KeyframesOff {
		if err := insertJob(ctx, tx, domain.JobKeyframes, part, 0, indexPriority, domain.JobDueNow); err != nil {
			return err
		}
	}
	if settings.Previews != domain.PreviewsOff {
		return insertJob(ctx, tx, domain.JobPreviews, part, 0, 0, settings.PreviewsDue)
	}
	return nil
}

// copyVersion gives an episode a version of its own of bytes another episode already holds, with
// what was read of them: the same bytes under two numbers are two episodes, each read from its own
// path, as Jellyfin keeps an item per path.
func copyVersion(ctx context.Context, tx db, lib uuid.UUID, settings analysis, from, itemID uuid.UUID, c Copy) error {
	var id uuid.UUID
	err := tx.QueryRow(ctx, `
		INSERT INTO versions (item_id, library_id, fingerprint, edition, label, container, width, height,
			video_codec, video_range, dv_profile, bitrate_kbps, size_bytes, duration_ms)
		SELECT $2, library_id, fingerprint, $3, $4, container, width, height,
			video_codec, video_range, dv_profile, bitrate_kbps, size_bytes, duration_ms
		FROM versions WHERE id = $1 RETURNING id`, from, itemID, optional(c.Edition), optional(c.Label)).Scan(&id)
	if err != nil {
		return err
	}
	stream := strings.TrimPrefix(streamColumns, "part_id, ")
	chapter := strings.TrimPrefix(chapterColumns, "part_id, ")
	for idx, part := range c.Parts {
		var source, copied uuid.UUID
		var video bool
		err := tx.QueryRow(ctx, `
			INSERT INTO parts (version_id, idx, size_bytes, duration_ms, offset_ms)
			SELECT $2, idx, size_bytes, duration_ms, offset_ms FROM parts WHERE version_id = $1 AND idx = $3
			RETURNING (SELECT id FROM parts WHERE version_id = $1 AND idx = $3), id,
				EXISTS (SELECT 1 FROM streams s JOIN parts p ON p.id = s.part_id WHERE p.version_id = $1 AND p.idx = $3 AND s.kind = 'video')`,
			from, id, int16(idx)).Scan(&source, &copied, &video)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO streams (part_id, `+stream+`) SELECT $2, `+stream+` FROM streams WHERE part_id = $1`,
			source, copied); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO chapters (part_id, `+chapter+`) SELECT $2, `+chapter+` FROM chapters WHERE part_id = $1`,
			source, copied); err != nil {
			return err
		}
		if err := locate(ctx, tx, lib, copied, part); err != nil {
			return err
		}
		if video {
			if err := indexVideo(ctx, tx, settings, copied); err != nil {
				return err
			}
		}
	}
	return saveSubtitles(ctx, tx, lib, id, c.Subtitles)
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
			BitrateKbps: optional(st.BitrateKbps),
		}
		if st.Language != language.Und {
			row.Language = optional(st.Language.String())
		}
		switch st.Kind {
		case domain.StreamVideo:
			row.Width, row.Height, row.FrameRate = optional(st.Width), optional(st.Height), &st.FrameRate
			row.VideoRange, row.Level, row.Interlaced = &st.Range, optional(st.Level), st.Interlaced
			if st.BitDepth > 0 {
				depth := int16(st.BitDepth)
				row.BitDepth = &depth
			}
			if dv := st.DolbyVision; dv != nil {
				profile, level, compat := int16(dv.Profile), int16(dv.Level), int16(dv.Compatibility.ID())
				row.DVProfile, row.DVLevel, row.DVCompatibility = &profile, &level, &compat
			}
		case domain.StreamAudio:
			row.Channels, row.ChannelLayout = optional(st.Channels), optional(st.ChannelLayout)
			row.SampleRate = optional(st.SampleRate)
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
