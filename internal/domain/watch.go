package domain

import "time"

// Reach is how far a play got, by Jellyfin's default rules.
type Reach string

const (
	// ReachStart is too near the start to resume from: under 5%, or anything under 5 minutes.
	ReachStart Reach = "start"
	// ReachResumable is somewhere to resume from.
	ReachResumable Reach = "resumable"
	// ReachEnd is past 90%: the title is watched.
	ReachEnd Reach = "end"
)

const (
	minResume        = 5
	maxResume        = 90
	minResumableTime = 5 * time.Minute
)

// ReachOf is how far a play that stopped at position got through a title running duration.
func ReachOf(position, duration time.Duration) Reach {
	switch {
	case duration <= 0:
		if position > 0 {
			return ReachResumable
		}
		return ReachStart
	case position*100 >= duration*maxResume:
		return ReachEnd
	case duration < minResumableTime || position*100 < duration*minResume:
		return ReachStart
	}
	return ReachResumable
}
