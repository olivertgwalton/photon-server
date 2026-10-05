package model

import (
	"time"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

type Webhook struct {
	ID        UUID `gorm:"type:uuid;default:uuidv7()"`
	URL       string
	Secret    string
	CreatedAt time.Time `gorm:"default:now()"`
}

type WebhookEvent struct {
	WebhookID UUID             `gorm:"type:uuid;primaryKey"`
	Kind      domain.EventKind `gorm:"primaryKey"`
}

type WebhookDelivery struct {
	ID        UUID `gorm:"type:uuid;default:uuidv7()"`
	WebhookID UUID `gorm:"type:uuid"`
	Kind      domain.EventKind
	Body      string
}
