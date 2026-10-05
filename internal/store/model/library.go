package model

import (
	"time"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

type Library struct {
	ID          UUID `gorm:"type:uuid;default:uuidv7()"`
	Name        string
	Kind        domain.LibraryKind
	Root        string
	Monitor     domain.Monitor `gorm:"default:realtime"`
	RefreshDays int16          `gorm:"default:30"`
	CreatedAt   time.Time
}
