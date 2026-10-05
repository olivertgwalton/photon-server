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
