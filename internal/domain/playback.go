package domain

import (
	"time"
	"uuid"
)

// PlayMethod is how a title reaches the player.
type PlayMethod string

const (
	// PlayDirect is the file as it is.
	PlayDirect PlayMethod = "direct"
	// PlayRemux is the file's streams copied into HLS segments.
	PlayRemux PlayMethod = "remux"
	// PlayTranscode is the file encoded again for the player.
	PlayTranscode PlayMethod = "transcode"
)

// PlayState is what the player says it is doing.
type PlayState string

const (
	StatePlaying PlayState = "playing"
	StatePaused  PlayState = "paused"
)

func ParsePlayState(s string) (PlayState, bool) {
	switch v := PlayState(s); v {
	case StatePlaying, StatePaused:
		return v, true
	}
	return "", false
}

// Playback is one profile playing one copy of a title, from play to stop.
type Playback struct {
	ID       uuid.UUID
	Profile  uuid.UUID
	Item     uuid.UUID
	Version  uuid.UUID
	Method   PlayMethod
	State    PlayState
	Position time.Duration
	Started  time.Time
	Updated  time.Time
}
