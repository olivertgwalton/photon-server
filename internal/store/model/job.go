package model

import (
	"time"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

type Job struct {
	ID         int64 `gorm:"primaryKey"`
	Kind       domain.JobKind
	Subject    UUID            `gorm:"type:uuid"`
	State      domain.JobState `gorm:"default:queued"`
	Priority   int16
	Attempts   int16
	RunAfter   time.Time `gorm:"default:now()"`
	LeaseUntil *time.Time
	NodeID     *UUID `gorm:"type:uuid"`
	LastError  *string
}
