package store

import (
	"context"
	"errors"
	"uuid"

	"gorm.io/gorm"

	"github.com/olivertgwalton/photon-server/internal/store/model"
)

// PlayPart is one file of a copy, in play order, where it starts on the copy's timeline.
type PlayPart struct {
	ID         uuid.UUID
	OffsetMS   int64
	DurationMS int64
}

// Playable answers the copy of a film or episode to play: the one asked for, else its longest on
// disk, and its parts. ErrNotFound for no such title, or none of its copies on disk.
func (s *Store) Playable(ctx context.Context, item, version uuid.UUID) (uuid.UUID, []PlayPart, error) {
	v, p := s.q.Version, s.q.Part
	q := v.WithContext(ctx).Where(v.ItemID.Eq(model.UUID(item)), v.MissingSince.IsNull())
	if version != (uuid.UUID{}) {
		q = q.Where(v.ID.Eq(model.UUID(version)))
	}
	row, err := q.Order(v.DurationMS.Desc(), v.ID).Take()
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return uuid.UUID{}, nil, ErrNotFound
	}
	if err != nil {
		return uuid.UUID{}, nil, err
	}
	parts, err := p.WithContext(ctx).Where(p.VersionID.Eq(row.ID)).Order(p.Idx).Find()
	if err != nil {
		return uuid.UUID{}, nil, err
	}
	out := make([]PlayPart, len(parts))
	for n, pt := range parts {
		out[n] = PlayPart{ID: uuid.UUID(pt.ID), OffsetMS: pt.OffsetMS, DurationMS: pt.DurationMS}
	}
	return uuid.UUID(row.ID), out, nil
}
