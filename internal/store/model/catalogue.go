package model

import (
	"time"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

type Folder struct {
	LibraryID   UUID   `gorm:"type:uuid;primaryKey"`
	Path        string `gorm:"primaryKey"`
	Fingerprint []byte
}

type Item struct {
	ID        UUID `gorm:"type:uuid;default:uuidv7()"`
	LibraryID UUID `gorm:"type:uuid"`
	Kind      domain.ItemKind
	Title     string
	SortTitle string
	Year      *int
	Folder    string
	AddedAt   time.Time `gorm:"default:now()"`

	ParentID      *UUID `gorm:"type:uuid"`
	SeasonNumber  *int
	EpisodeNumber *int
	EpisodeEnd    *int
	AirDate       *time.Time `gorm:"type:date"`
}

type ExternalID struct {
	ItemID   UUID            `gorm:"type:uuid;primaryKey"`
	Provider domain.Provider `gorm:"primaryKey"`
	Value    string
	Source   domain.IDSource
}

type Version struct {
	ID           UUID `gorm:"type:uuid;default:uuidv7()"`
	ItemID       UUID `gorm:"type:uuid"`
	LibraryID    UUID `gorm:"type:uuid"`
	Fingerprint  []byte
	Edition      *string
	Label        *string
	Container    string
	Width        *int
	Height       *int
	VideoCodec   *string
	VideoRange   *domain.Range
	DVProfile    *int16 `gorm:"column:dv_profile"`
	BitrateKbps  int
	SizeBytes    int64
	DurationMS   int64 `gorm:"column:duration_ms"`
	MissingSince *time.Time
}

type Part struct {
	ID         UUID `gorm:"type:uuid;default:uuidv7()"`
	VersionID  UUID `gorm:"type:uuid"`
	Idx        int16
	LibraryID  UUID `gorm:"type:uuid"`
	RelPath    string
	SizeBytes  int64
	MtimeNS    int64 `gorm:"column:mtime_ns"`
	DurationMS int64 `gorm:"column:duration_ms"`
	OffsetMS   int64 `gorm:"column:offset_ms"`
}

type Stream struct {
	PartID          UUID `gorm:"type:uuid;primaryKey"`
	Idx             int  `gorm:"primaryKey"`
	Kind            domain.StreamKind
	Codec           string
	Profile         *string
	Language        *string
	Title           *string
	IsDefault       bool
	Forced          bool
	HearingImpaired bool
	Commentary      bool
	Width           *int
	Height          *int
	FrameRate       *float64
	VideoRange      *domain.Range
	DVProfile       *int16 `gorm:"column:dv_profile"`
	DVLevel         *int16 `gorm:"column:dv_level"`
	DVCompatibility *int16 `gorm:"column:dv_compatibility"`
	Channels        *int
	ChannelLayout   *string
	SampleRate      *int
	BitrateKbps     *int
}

type Chapter struct {
	PartID  UUID  `gorm:"type:uuid;primaryKey"`
	Idx     int   `gorm:"primaryKey"`
	StartMS int64 `gorm:"column:start_ms"`
	EndMS   int64 `gorm:"column:end_ms"`
	Title   *string
}
