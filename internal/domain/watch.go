package domain

import "time"

// Reach is how far a play got, by Jellyfin's default rules.
type Reach string

const (
	// ReachStart is too near the start to resume from: under 5%.
	ReachStart Reach = "start"
	// ReachResumable is somewhere to resume from.
	ReachResumable Reach = "resumable"
	// ReachEnd is past 90%, within a second of the end, or anywhere past the start of a title under
	// 5 minutes: the title is watched.
	ReachEnd Reach = "end"
)

func Reaches() []Reach {
	return []Reach{ReachStart, ReachResumable, ReachEnd}
}

const (
	minResume        = 5
	maxResume        = 90
	minResumableTime = 5 * time.Minute
)

// ReachOf is how far a play that stopped at position got through a title running duration.
func ReachOf(position, duration time.Duration) Reach {
	switch {
	case duration <= 0:
		// Jellyfin counts a title it cannot time as watched by any report; players report from the
		// start, so here it is somewhere to resume from instead.
		if position > 0 {
			return ReachResumable
		}
		return ReachStart
	case position*100 < duration*minResume:
		return ReachStart
	case position*100 > duration*maxResume || position >= duration-time.Second || duration < minResumableTime:
		return ReachEnd
	}
	return ReachResumable
}
