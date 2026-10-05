package task

import "time"

type TriggerKind string

const (
	// TriggerEvery runs a task when Every has passed since it last started, and as soon as the
	// scheduler starts if it never has.
	TriggerEvery TriggerKind = "every"
	// TriggerDaily runs a task once a day at At past local midnight.
	TriggerDaily TriggerKind = "daily"
)

type Trigger struct {
	Kind  TriggerKind
	Every time.Duration
	At    time.Duration
}

// due reports whether a trigger has fired since the task last started. A run missed while the
// server was down is due at the next chance, not at the next occurrence.
func (t Trigger) due(last, now time.Time) bool {
	switch t.Kind {
	case TriggerEvery:
		return last.IsZero() || !now.Before(last.Add(t.Every))
	case TriggerDaily:
		y, m, d := now.Date()
		today := time.Date(y, m, d, 0, 0, 0, 0, now.Location()).Add(t.At)
		latest := today
		if now.Before(today) {
			latest = today.AddDate(0, 0, -1)
		}
		return last.Before(latest)
	}
	return false
}
