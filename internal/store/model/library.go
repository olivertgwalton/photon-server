package model

import (
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

type Library struct {
	ID          uuid.UUID
	Name        string
	Kind        domain.LibraryKind
	Root        string
	Monitor     domain.Monitor
	RefreshDays int16
	Previews    domain.PreviewLevel
	Markers     domain.MarkerDetection
	Keyframes   domain.KeyframeMode
	Themes      domain.ThemeLookup
	CreatedAt   time.Time
}
