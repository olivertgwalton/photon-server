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

// PlayCopy is a copy to play: its container and bitrate, its parts, and its first part's streams.
type PlayCopy struct {
	Version     uuid.UUID
	Container   string
	BitrateKbps int
	Parts       []PlayPart
	Streams     []media.Stream
}

// Playable answers the copy of a film or episode to play: the one asked for, else its longest on
// disk. ErrNotFound for no such title, or none of its copies on disk.
func (s *Store) Playable(ctx context.Context, item, version uuid.UUID) (PlayCopy, error) {
	v, p, st := s.q.Version, s.q.Part, s.q.Stream
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
	c := PlayCopy{Version: uuid.UUID(row.ID), Container: row.Container, BitrateKbps: row.BitrateKbps}
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
		BitDepth: int(deref(t.BitDepth)), Level: deref(t.Level), Range: deref(t.VideoRange),
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

// Keyframes answers a part's video keyframe times, or false where they have not been indexed yet.
func (s *Store) Keyframes(ctx context.Context, part uuid.UUID) ([]int64, bool, error) {
	var pts []int64
	err := s.pool.QueryRow(ctx, `SELECT pts_ms FROM keyframes WHERE part_id = $1`, part.String()).Scan(&pts)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	return pts, err == nil, err
}
