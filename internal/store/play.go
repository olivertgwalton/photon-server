package store

import (
	"cmp"
	"context"
	"uuid"

	"golang.org/x/text/language"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
)

// PlayPart is one file of a copy, in play order, where it starts on the copy's timeline.
type PlayPart struct {
	ID         uuid.UUID
	OffsetMS   int64
	DurationMS int64
}

// PlayCopy is a copy to play: what it is, its container, bitrate and length, its parts, its first
// part's streams, and the subtitle files beside it.
type PlayCopy struct {
	Version     uuid.UUID
	Edition     string
	Label       string
	DurationMS  int64
	Container   string
	BitrateKbps int
	Parts       []PlayPart
	Streams     []domain.Stream
	Subtitles   []PlaySubtitle
}

// PlaySubtitle is a subtitle file beside a copy, timed on the copy's whole timeline.
type PlaySubtitle struct {
	ID              uuid.UUID
	Codec           string
	Language        language.Tag
	Title           string
	Default         bool
	Forced          bool
	HearingImpaired bool
}

// Playable answers the copy of a film or episode to play: the one asked for, else its longest on
// disk. ErrNotFound for no such title, one the profile may not see, or none of its copies on disk.
func (s *Store) Playable(ctx context.Context, profile, item, version uuid.UUID) (PlayCopy, error) {
	rows, err := queryRows[model.Version](ctx, s.pool, `
		SELECT `+versionColumns+` FROM versions
		WHERE item_id = $1 AND missing_since IS NULL AND ($3::uuid IS NULL OR id = $3)
			AND EXISTS (SELECT 1 FROM items i, viewer($2) v WHERE i.id = item_id AND sees(v, i))
		ORDER BY duration_ms DESC, id LIMIT 1`, item, profile, optional(version))
	if err != nil || len(rows) == 0 {
		return PlayCopy{}, cmp.Or(err, ErrNotFound)
	}
	row := rows[0]
	parts, err := queryRows[model.Part](ctx, s.pool, `SELECT `+partColumns+` FROM parts WHERE version_id = $1 ORDER BY idx`, row.ID)
	if err != nil || len(parts) == 0 {
		return PlayCopy{}, cmp.Or(err, ErrNotFound)
	}
	streams, err := s.partStreams(ctx, parts[0].ID)
	if err != nil {
		return PlayCopy{}, err
	}
	subs, err := queryRows[model.SubtitleFile](ctx, s.pool, `
		SELECT `+subtitleFileColumns+` FROM subtitle_files WHERE version_id = $1 ORDER BY rel_path`, row.ID)
	if err != nil {
		return PlayCopy{}, err
	}
	c := PlayCopy{
		Version: row.ID, Edition: deref(row.Edition), Label: deref(row.Label), DurationMS: row.DurationMS,
		Container: row.Container, BitrateKbps: row.BitrateKbps, Streams: streams,
	}
	for _, f := range subs {
		c.Subtitles = append(c.Subtitles, playSubtitle(f))
	}
	for _, pt := range parts {
		c.Parts = append(c.Parts, PlayPart{ID: pt.ID, OffsetMS: pt.OffsetMS, DurationMS: pt.DurationMS})
	}
	return c, nil
}

func playSubtitle(f *model.SubtitleFile) PlaySubtitle {
	lang, _ := language.Parse(deref(f.Language))
	return PlaySubtitle{
		ID: f.ID, Codec: f.Codec, Language: lang, Title: deref(f.Title), Default: f.IsDefault,
		Forced: f.Forced, HearingImpaired: f.HearingImpaired,
	}
}

// mediaStream is a stream as it was probed.
func mediaStream(t *model.Stream) domain.Stream {
	lang, _ := language.Parse(deref(t.Language))
	m := domain.Stream{
		Index: t.Idx, Kind: t.Kind, Codec: t.Codec, Profile: deref(t.Profile), Language: lang, Title: deref(t.Title),
		Default: t.IsDefault, Forced: t.Forced, HearingImpaired: t.HearingImpaired, Commentary: t.Commentary,
		Width: deref(t.Width), Height: deref(t.Height), FrameRate: deref(t.FrameRate),
		BitDepth: int(deref(t.BitDepth)), Level: deref(t.Level), Range: deref(t.VideoRange), Interlaced: t.Interlaced,
		Channels: deref(t.Channels), ChannelLayout: deref(t.ChannelLayout), SampleRate: deref(t.SampleRate),
		BitrateKbps: deref(t.BitrateKbps),
	}
	if t.DVProfile != nil {
		m.DolbyVision = &domain.DolbyVision{
			Profile: int(*t.DVProfile), Level: int(deref(t.DVLevel)), Compatibility: int(deref(t.DVCompatibility)),
		}
	}
	return m
}

// PartStreams answers a part's streams, as they were probed, or ErrNotFound for no such part:
// every part has some.
func (s *Store) PartStreams(ctx context.Context, part uuid.UUID) ([]domain.Stream, error) {
	streams, err := s.partStreams(ctx, part)
	if err == nil && len(streams) == 0 {
		return nil, ErrNotFound
	}
	return streams, err
}

func (s *Store) partStreams(ctx context.Context, part uuid.UUID) ([]domain.Stream, error) {
	rows, err := queryRows[model.Stream](ctx, s.pool, `SELECT `+streamColumns+` FROM streams WHERE part_id = $1 ORDER BY idx`, part)
	var streams []domain.Stream
	for _, t := range rows {
		streams = append(streams, mediaStream(t))
	}
	return streams, err
}

// SubtitleFile answers where a subtitle file is: its library's root, and its path within it; no
// root for one fetched, whose text is kept here (see SubtitleBody).
func (s *Store) SubtitleFile(ctx context.Context, id uuid.UUID) (root, rel string, err error) {
	err = s.pool.QueryRow(ctx, `
		SELECT CASE WHEN f.body IS NULL THEN l.root ELSE '' END, f.rel_path
		FROM subtitle_files f JOIN libraries l ON l.id = f.library_id WHERE f.id = $1`,
		id).Scan(&root, &rel)
	return root, rel, found(err)
}

// VisiblePartFile is where a part's bytes are, as PartFile answers, or ErrNotFound unless the
// profile may see its title.
func (s *Store) VisiblePartFile(ctx context.Context, profile, part uuid.UUID) (root, rel string, err error) {
	err = s.pool.QueryRow(ctx, `
		SELECT l.root, f.rel_path FROM part_files f JOIN libraries l ON l.id = f.library_id
			JOIN parts p ON p.id = f.part_id JOIN versions v ON v.id = p.version_id
			JOIN items i ON i.id = v.item_id, viewer($2) asking
		WHERE f.part_id = $1 AND sees(asking, i) ORDER BY f.rel_path LIMIT 1`,
		part, profile).Scan(&root, &rel)
	return root, rel, found(err)
}

// Subtitle answers what a subtitle file beside a copy is, or ErrNotFound.
func (s *Store) Subtitle(ctx context.Context, id uuid.UUID) (PlaySubtitle, error) {
	f, err := queryRows[model.SubtitleFile](ctx, s.pool, `SELECT `+subtitleFileColumns+` FROM subtitle_files WHERE id = $1`, id)
	if err != nil || len(f) == 0 {
		return PlaySubtitle{}, cmp.Or(err, ErrNotFound)
	}
	return playSubtitle(f[0]), nil
}

// PartKeyframes is how a part's library finds keyframes, and those found: none where the part has
// none known, nil where it has not been read for them yet.
type PartKeyframes struct {
	Mode  domain.KeyframeMode
	PtsMS []int64
}

// Keyframes answers a part's keyframes, or ErrNotFound for a part gone.
func (s *Store) Keyframes(ctx context.Context, part uuid.UUID) (PartKeyframes, error) {
	var k PartKeyframes
	var mode string
	err := s.pool.QueryRow(ctx, `
		SELECT l.keyframes, k.pts_ms
		FROM parts p JOIN versions v ON v.id = p.version_id JOIN libraries l ON l.id = v.library_id
		LEFT JOIN keyframes k ON k.part_id = p.id
		WHERE p.id = $1`, part).Scan(&mode, &k.PtsMS)
	k.Mode = domain.KeyframeMode(mode)
	return k, found(err)
}
