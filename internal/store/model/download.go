package model

import (
	"time"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

type Conversion struct {
	ID             UUID `gorm:"type:uuid;default:uuidv7()"`
	PartID         UUID `gorm:"type:uuid"`
	MaxBitrateKbps int
	MaxWidth       int
	State          domain.DownloadState `gorm:"default:queued"`
	Progress       float64
	SizeBytes      *int64
	Error          *string
	NodeID         *UUID `gorm:"type:uuid"`
	FinishedAt     *time.Time
}

type Download struct {
	ID           UUID      `gorm:"type:uuid;default:uuidv7()"`
	ProfileID    UUID      `gorm:"type:uuid"`
	ItemID       UUID      `gorm:"type:uuid"`
	PartID       UUID      `gorm:"type:uuid"`
	ConversionID *UUID     `gorm:"type:uuid"`
	CreatedAt    time.Time `gorm:"default:now()"`
}
