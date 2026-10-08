package domain

import (
	"time"
	"uuid"
)

// RestorePhase is how far a restore asked for from the web has got.
type RestorePhase string

const (
	// RestoreStopping is every node stopping, so the one restoring has the database to itself.
	RestoreStopping RestorePhase = "stopping"
	// RestoreRestoring is the dump being restored.
	RestoreRestoring RestorePhase = "restoring"
)

func RestorePhases() []RestorePhase {
	return []RestorePhase{RestoreStopping, RestoreRestoring}
}

// Restore is a restore under way: the dump, the node that keeps it and restores it, and when it
// was asked for.
type Restore struct {
	Dump    string
	Node    uuid.UUID
	Started time.Time
	Phase   RestorePhase
}

// RestoreResult is how a restore ended.
type RestoreResult string

const (
	RestoreSucceeded RestoreResult = "succeeded"
	RestoreFailed    RestoreResult = "failed"
)

func RestoreResults() []RestoreResult {
	return []RestoreResult{RestoreSucceeded, RestoreFailed}
}

// RestoreOutcome is how the last restore ended, and why where it failed.
type RestoreOutcome struct {
	Dump   string
	At     time.Time
	Result RestoreResult
	Reason string
}
