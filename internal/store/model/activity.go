package model

import (
	"time"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

type Activity struct {
	ID        UUID      `gorm:"type:uuid;default:uuidv7()"`
	At        time.Time `gorm:"default:now()"`
	Kind      domain.EventKind
	ProfileID *UUID  `gorm:"type:uuid"`
	ItemID    *UUID  `gorm:"type:uuid"`
	LibraryID *UUID  `gorm:"type:uuid"`
	Details   []byte `gorm:"type:jsonb;default:'{}'"`
}

func (Activity) TableName() string { return "activity" }
