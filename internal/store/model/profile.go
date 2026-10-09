package model

import (
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

type Profile struct {
	ID   uuid.UUID
	Name string
	Role domain.Role
	// PasswordHash is nil for a profile a sign-in provider's account was given, until it sets one.
	PasswordHash *string
	PinHash      *string
	AvatarID     *uuid.UUID
	ManagedBy    *uuid.UUID
}

type Play struct {
	ID         uuid.UUID
	ProfileID  uuid.UUID
	ItemID     uuid.UUID
	Method     domain.PlayMethod
	StartedAt  time.Time
	StoppedAt  time.Time
	PositionMS int64
}
