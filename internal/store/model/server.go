package model

import "time"

type Server struct {
	ID         UUID `gorm:"type:uuid"`
	CreatedAt  time.Time
	SigningKey []byte `gorm:"->"`
}

func (Server) TableName() string { return "server" }
