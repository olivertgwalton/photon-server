package task

import "time"

type TriggerKind string

const (
	// TriggerEvery runs a task when Every has passed since it last started, and as soon as the
	// scheduler starts if it never has.
	TriggerEvery TriggerKind = "every"
	// TriggerDaily runs a task once a day at At past local midnight.
	TriggerDaily TriggerKind = "daily"
	// TriggerWindow runs a task once a day as the maintenance window Opens answers opens.
	TriggerWindow TriggerKind = "window"
)

type Trigger struct {
	Kind  TriggerKind
	Every time.Duration
	At    time.Duration
	// Opens answers when past midnight the maintenance window opens, in its zone, or false while
	// that is not known.
	Opens func() (at time.Duration, zone *time.Location, ok bool)
}

// due reports whether a trigger has fired since the task last started. A run missed while the
// server was down is due at the next chance, not at the next occurrence.
func (t Trigger) due(last, now time.Time) bool {
	switch t.Kind {
	case TriggerEvery:
		return last.IsZero() || !now.Before(last.Add(t.Every))
	case TriggerDaily:
		return last.Before(latest(t.At, now))
	case TriggerWindow:
		at, zone, ok := t.Opens()
		return ok && last.Before(latest(at, now.In(zone)))
	}
	return false
}

// next answers when a trigger next fires after a task last started at last, given it has not by
// now.
func (t Trigger) next(last, now time.Time) time.Time {
	switch t.Kind {
	case TriggerEvery:
		return last.Add(t.Every)
	case TriggerDaily:
		return latest(t.At, now).AddDate(0, 0, 1)
	case TriggerWindow:
		if at, zone, ok := t.Opens(); ok {
			return latest(at, now.In(zone)).AddDate(0, 0, 1)
		}
	}
	return time.Time{}
}

// latest answers when at past midnight last came by now, in now's zone.
func latest(at time.Duration, now time.Time) time.Time {
	y, m, d := now.Date()
	today := time.Date(y, m, d, 0, 0, 0, 0, now.Location()).Add(at)
	if now.Before(today) {
		return today.AddDate(0, 0, -1)
	}
	return today
}
