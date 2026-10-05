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

// VideoPlan is the video stream played, by its index in the file, and what becomes of its Dolby
// Vision.
type VideoPlan struct {
	Stream      int
	Codec       string
	DolbyVision DolbyVisionHandling
}

// DolbyVisionHandling is what a copy does with a stream's Dolby Vision.
type DolbyVisionHandling string

const (
	// DolbyVisionNone is a stream with none.
	DolbyVisionNone DolbyVisionHandling = ""
	// DolbyVisionKeep carries it to a client that shows it.
	DolbyVisionKeep DolbyVisionHandling = "keep"
	// DolbyVisionStrip drops it for the base layer, to a client that shows only that.
	DolbyVisionStrip DolbyVisionHandling = "strip"
)

// AudioPlan is the audio stream played, by its index in the file, and what it is encoded to where
// it is not copied.
type AudioPlan struct {
	Stream int
	Encode *AudioEncode
}

// AudioEncode is audio encoded again: the codec, its channels and its bitrate.
type AudioEncode struct {
	Codec       string
	Channels    int
	BitrateKbps int
}
