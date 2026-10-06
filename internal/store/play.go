package store

import (
	"cmp"
	"context"
	"errors"
	"uuid"

	"github.com/jackc/pgx/v5"
	"golang.org/x/text/language"
	"gorm.io/gen/field"
	"gorm.io/gorm"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/media"
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
	Streams     []media.Stream
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
	v, p, st, sf := s.q.Version, s.q.Part, s.q.Stream, s.q.SubtitleFile
	q := v.WithContext(ctx).Where(v.ItemID.Eq(model.UUID(item)), v.MissingSince.IsNull(),
		field.NewUnsafeFieldRaw("EXISTS (SELECT 1 FROM items i, viewer(?) v WHERE i.id = item_id AND sees(v, i))", profile.String()))
	if version != (uuid.UUID{}) {
		q = q.Where(v.ID.Eq(model.UUID(version)))
	}
	row, err := q.Order(v.DurationMS.Desc(), v.ID).Take()
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return PlayCopy{}, ErrNotFound
	}
	if err != nil {
		return PlayCopy{}, err
	}
	parts, err := p.WithContext(ctx).Where(p.VersionID.Eq(row.ID)).Order(p.Idx).Find()
	if err != nil || len(parts) == 0 {
		return PlayCopy{}, cmp.Or(err, ErrNotFound)
	}
	streams, err := st.WithContext(ctx).Where(st.PartID.Eq(parts[0].ID)).Order(st.Idx).Find()
	if err != nil {
		return PlayCopy{}, err
	}
	subs, err := sf.WithContext(ctx).Where(sf.VersionID.Eq(row.ID)).Order(sf.RelPath).Find()
	if err != nil {
		return PlayCopy{}, err
	}
	c := PlayCopy{
		Version: uuid.UUID(row.ID), Edition: deref(row.Edition), Label: deref(row.Label), DurationMS: row.DurationMS,
		Container: row.Container, BitrateKbps: row.BitrateKbps,
	}
	for _, f := range subs {
		c.Subtitles = append(c.Subtitles, playSubtitle(f))
	}
	for _, pt := range parts {
		c.Parts = append(c.Parts, PlayPart{ID: uuid.UUID(pt.ID), OffsetMS: pt.OffsetMS, DurationMS: pt.DurationMS})
	}
	for _, t := range streams {
		c.Streams = append(c.Streams, mediaStream(t))
	}
	return c, nil
}

func playSubtitle(f *model.SubtitleFile) PlaySubtitle {
	lang, _ := language.Parse(deref(f.Language))
	return PlaySubtitle{
		ID: uuid.UUID(f.ID), Codec: f.Codec, Language: lang, Title: deref(f.Title), Default: f.IsDefault,
		Forced: f.Forced, HearingImpaired: f.HearingImpaired,
	}
}

// mediaStream is a stream as it was probed.
func mediaStream(t *model.Stream) media.Stream {
	lang, _ := language.Parse(deref(t.Language))
	m := media.Stream{
		Index: t.Idx, Kind: t.Kind, Codec: t.Codec, Profile: deref(t.Profile), Language: lang, Title: deref(t.Title),
		Default: t.IsDefault, Forced: t.Forced, HearingImpaired: t.HearingImpaired, Commentary: t.Commentary,
		Width: deref(t.Width), Height: deref(t.Height), FrameRate: deref(t.FrameRate),
		BitDepth: int(deref(t.BitDepth)), Level: deref(t.Level), Range: deref(t.VideoRange), Interlaced: t.Interlaced,
		Channels: deref(t.Channels), ChannelLayout: deref(t.ChannelLayout), SampleRate: deref(t.SampleRate),
		BitrateKbps: deref(t.BitrateKbps),
	}
	if t.DVProfile != nil {
		m.DolbyVision = &media.DolbyVision{
			Profile: int(*t.DVProfile), Level: int(deref(t.DVLevel)), Compatibility: int(deref(t.DVCompatibility)),
		}
	}
	return m
}

// SubtitleFile answers where a subtitle file is: its library's root, and its path within it.
func (s *Store) SubtitleFile(ctx context.Context, id uuid.UUID) (root, rel string, err error) {
	f, l := s.q.SubtitleFile, s.q.Library
	var row struct {
		Root    string
		RelPath string
	}
	err = f.WithContext(ctx).Select(l.Root, f.RelPath).Join(l, l.ID.EqCol(f.LibraryID)).
		Where(f.ID.Eq(model.UUID(id))).Limit(1).Scan(&row)
	if err == nil && row.RelPath == "" {
		err = ErrNotFound
	}
	return row.Root, row.RelPath, err
}

// VisiblePartFile is where a part's bytes are, as PartFile answers, or ErrNotFound unless the
// profile may see its title.
func (s *Store) VisiblePartFile(ctx context.Context, profile, part uuid.UUID) (root, rel string, err error) {
	err = s.pool.QueryRow(ctx, `
		SELECT l.root, f.rel_path FROM part_files f JOIN libraries l ON l.id = f.library_id
			JOIN parts p ON p.id = f.part_id JOIN versions v ON v.id = p.version_id
			JOIN items i ON i.id = v.item_id, viewer($2) asking
		WHERE f.part_id = $1 AND sees(asking, i) ORDER BY f.rel_path LIMIT 1`,
		part.String(), profile.String()).Scan(&root, &rel)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrNotFound
	}
	return root, rel, err
}

// Subtitle answers what a subtitle file beside a copy is, or ErrNotFound.
func (s *Store) Subtitle(ctx context.Context, id uuid.UUID) (PlaySubtitle, error) {
	sf := s.q.SubtitleFile
	f, err := sf.WithContext(ctx).Where(sf.ID.Eq(model.UUID(id))).Take()
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return PlaySubtitle{}, ErrNotFound
	}
	if err != nil {
		return PlaySubtitle{}, err
	}
	return playSubtitle(f), nil
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
		WHERE p.id = $1`, part.String()).Scan(&mode, &k.PtsMS)
	if errors.Is(err, pgx.ErrNoRows) {
		return k, ErrNotFound
	}
	k.Mode = domain.KeyframeMode(mode)
	return k, err
}
