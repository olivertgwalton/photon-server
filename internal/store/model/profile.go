package model

import (
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

type Profile struct {
	ID           uuid.UUID
	Name         string
	Role         domain.Role
	PasswordHash *string
	PinHash      *string
	CreatedAt    time.Time
	MaxAge       *int16
	Unrated      domain.Unrated
	AvatarID     *uuid.UUID
}

type Play struct {
	ID         uuid.UUID
	ProfileID  uuid.UUID
	ItemID     uuid.UUID
	VersionID  *uuid.UUID
	Method     domain.PlayMethod
	StartedAt  time.Time
	StoppedAt  time.Time
	PositionMS int64
}
