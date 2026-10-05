package model

import (
	"time"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

type Profile struct {
	ID           UUID `gorm:"type:uuid;default:uuidv7()"`
	Name         string
	Role         domain.Role
	PasswordHash *string
	PinHash      *string
	CreatedAt    time.Time `gorm:"default:now()"`
	MaxAge       *int16
	Unrated      domain.Unrated `gorm:"default:allow"`
}

type ProfileLibrary struct {
	ProfileID UUID `gorm:"type:uuid;primaryKey"`
	LibraryID UUID `gorm:"type:uuid;primaryKey"`
}

type DeviceSession struct {
	ID         UUID `gorm:"type:uuid;default:uuidv7()"`
	TokenHash  []byte
	ProfileID  UUID `gorm:"type:uuid"`
	DeviceName string
	Client     string
	CreatedAt  time.Time `gorm:"default:now()"`
	LastSeenAt time.Time `gorm:"default:now()"`
	ExpiresAt  time.Time
}

type Play struct {
	ID         UUID  `gorm:"type:uuid;default:uuidv7()"`
	ProfileID  UUID  `gorm:"type:uuid"`
	ItemID     UUID  `gorm:"type:uuid"`
	VersionID  *UUID `gorm:"type:uuid"`
	Method     domain.PlayMethod
	StartedAt  time.Time
	StoppedAt  time.Time
	PositionMS int64 `gorm:"column:position_ms"`
}
