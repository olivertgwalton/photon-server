package model

import "time"

type Server struct {
	ID        UUID `gorm:"type:uuid"`
	CreatedAt time.Time
}

func (Server) TableName() string { return "server" }
