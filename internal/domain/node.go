package domain

import (
	"time"
	"uuid"
)

// Node is a server node as it tells the others of itself: where its peers reach it, what it
// encodes with, and how many videos it encodes now, said every little while and as that changes.
type Node struct {
	ID      uuid.UUID
	Address string
	Seen    time.Time
	// Name is its host's, to tell nodes apart by.
	Name    string
	Encoder Encoder
	// Transcodes are the videos it encodes now, Conversions of them for downloads, of at most
	// Limit at once; a Limit of zero is none.
	Transcodes, Conversions, Limit int
	LimitSource                    LimitSource
}

// Encoder is what a node encodes video with.
type Encoder struct {
	Acceleration Acceleration
	HEVC         HEVCEncoding
	// Libass is whether it can draw styled subtitles into video.
	Libass bool
}

// LimitSource is where a node's limit on transcodes at once comes from.
type LimitSource string

const (
	// LimitAutomatic is what its encoder keeps up with.
	LimitAutomatic LimitSource = "automatic"
	// LimitEnvironment is PHOTON_MAX_TRANSCODES.
	LimitEnvironment LimitSource = "environment"
)

func LimitSources() []LimitSource {
	return []LimitSource{LimitAutomatic, LimitEnvironment}
}
