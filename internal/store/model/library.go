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
	Monitor     domain.Monitor         `gorm:"default:realtime"`
	RefreshDays int16                  `gorm:"default:30"`
	Previews    domain.PreviewLevel    `gorm:"default:all"`
	Markers     domain.MarkerDetection `gorm:"default:all"`
	Keyframes   domain.KeyframeMode    `gorm:"default:index"`
	Themes      domain.ThemeLookup     `gorm:"default:local"`
	CreatedAt   time.Time
}
