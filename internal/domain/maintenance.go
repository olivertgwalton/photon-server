package domain

import (
	"errors"
	"fmt"
	"time"
)

// Timing is when work that reads media, and that no one is waiting on, is done: inside the
// maintenance window alone, or there and as soon as a part is added, as Plex offers for chapter
// thumbnails and intro markers. A library that wants none of it says so with its own level, off.
type Timing string

const (
	TimingWindow         Timing = "window"
	TimingWindowAndAdded Timing = "window_and_added"
)

func Timings() []Timing {
	return []Timing{TimingWindow, TimingWindowAndAdded}
}

// Maintenance is the server's maintenance window, from StartHour to EndHour of the day in Zone,
// past midnight where it ends before it starts, and when previews are made and intros and credits
// found by sound.
type Maintenance struct {
	StartHour int
	EndHour   int
	Zone      *time.Location
	Previews  Timing
	Markers   Timing
}

// Check refuses a window of no hours, of an hour no day has, or without a timing for each kind of
// work.
func (m Maintenance) Check() error {
	switch {
	case m.StartHour < 0 || m.StartHour > 23 || m.EndHour < 0 || m.EndHour > 23:
		return errors.New("the window's hours are from 0 to 23")
	case m.StartHour == m.EndHour:
		return errors.New("the window ends at another hour than it starts")
	}
	if _, err := Parse("previews", string(m.Previews), Timings()); err != nil {
		return err
	}
	_, err := Parse("markers", string(m.Markers), Timings())
	return err
}

// Holds reports whether t is inside the window.
func (m Maintenance) Holds(t time.Time) bool {
	h := t.In(m.Zone).Hour()
	if m.StartHour < m.EndHour {
		return h >= m.StartHour && h < m.EndHour
	}
	return h >= m.StartHour || h < m.EndHour
}

// ParseZone answers the time zone of an IANA name, such as Europe/London or UTC. The server's own
// local zone is refused by name: every node keeps the window in the same zone.
func ParseZone(name string) (*time.Location, error) {
	if name == "" || name == "Local" {
		return nil, fmt.Errorf("time zone %q is not an IANA name", name)
	}
	zone, err := time.LoadLocation(name)
	if err != nil {
		return nil, fmt.Errorf("time zone %q is not known", name)
	}
	return zone, nil
}
