package store

import (
	"cmp"
	"context"
	"errors"
	"uuid"

	"github.com/jackc/pgx/v5"
	"golang.org/x/text/language"
	"gorm.io/gorm"

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
	if ok, err := s.visible(ctx, profile, item); err != nil || !ok {
		return PlayCopy{}, cmp.Or(err, ErrNotFound)
	}
	v, p, st, sf := s.q.Version, s.q.Part, s.q.Stream, s.q.SubtitleFile
	q := v.WithContext(ctx).Where(v.ItemID.Eq(model.UUID(item)), v.MissingSince.IsNull())
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
		lang, _ := language.Parse(deref(f.Language))
		c.Subtitles = append(c.Subtitles, PlaySubtitle{
			ID: uuid.UUID(f.ID), Codec: f.Codec, Language: lang, Title: deref(f.Title), Default: f.IsDefault,
			Forced: f.Forced, HearingImpaired: f.HearingImpaired,
		})
	}
	for _, pt := range parts {
		c.Parts = append(c.Parts, PlayPart{ID: uuid.UUID(pt.ID), OffsetMS: pt.OffsetMS, DurationMS: pt.DurationMS})
	}
	for _, t := range streams {
		c.Streams = append(c.Streams, mediaStream(t))
	}
	return c, nil
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

// Keyframes answers a part's video keyframe times, or false where they have not been indexed yet.
func (s *Store) Keyframes(ctx context.Context, part uuid.UUID) ([]int64, bool, error) {
	var pts []int64
	err := s.pool.QueryRow(ctx, `SELECT pts_ms FROM keyframes WHERE part_id = $1`, part.String()).Scan(&pts)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	return pts, err == nil, err
}
