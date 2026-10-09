package domain

import "time"

// Tracker is a service a profile keeps what it watches on beside the server, as Trakt and Simkl
// keep it across every app and server a person uses.
type Tracker string

const (
	TrackerTrakt Tracker = "trakt"
	TrackerSimkl Tracker = "simkl"
)

func Trackers() []Tracker {
	return []Tracker{TrackerTrakt, TrackerSimkl}
}

// TrackerAccount is the account a profile linked on a tracker, named as the tracker named it then.
type TrackerAccount struct {
	Tracker  Tracker
	Username string
	LinkedAt time.Time
}
