package model

import (
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

type Activity struct {
	ID        uuid.UUID
	At        time.Time
	Kind      domain.EventKind
	ProfileID *uuid.UUID
	ItemID    *uuid.UUID
	LibraryID *uuid.UUID
	Details   []byte
}
