package domain

import (
	"slices"
	"time"
	"uuid"
)

// EventKind is something that happened on the server, told live to an admin's dashboard and, for
// those Logged, kept in the activity log.
type EventKind string

const (
	EventPlaybackStarted EventKind = "playback.started"
	EventPlaybackPaused  EventKind = "playback.paused"
	EventPlaybackResumed EventKind = "playback.resumed"
	EventPlaybackStopped EventKind = "playback.stopped"
	EventSignedIn        EventKind = "auth.signed_in"
	EventSignInRefused   EventKind = "auth.sign_in_refused"
	EventProfileAdded    EventKind = "profile.added"
	EventProfileRemoved  EventKind = "profile.removed"
	EventLibraryAdded    EventKind = "library.added"
	EventLibraryRemoved  EventKind = "library.removed"
	EventLibraryScanned  EventKind = "library.scanned"
	// EventTitlesAdded is the films and episodes a scan found that the library did not have.
	EventTitlesAdded EventKind = "library.titles_added"
	// EventScanProgress is how far a scan has got; it says so at most every scanProgressEvery.
	EventScanProgress EventKind = "scan.progress"
	EventTaskStarted  EventKind = "task.started"
	// EventTaskFinished is a task that succeeded or was cancelled; one that failed is EventTaskFailed.
	EventTaskFinished EventKind = "task.finished"
	EventTaskFailed   EventKind = "task.failed"
	EventBackupMade   EventKind = "backup.made"
	EventJobStarted   EventKind = "job.started"
	EventJobFinished  EventKind = "job.finished"
	// EventJobFailed is an attempt that failed and will be tried again; EventJobDead is the last.
	EventJobFailed EventKind = "job.failed"
	EventJobDead   EventKind = "job.dead"
)

func EventKinds() []EventKind {
	return []EventKind{
		EventPlaybackStarted, EventPlaybackPaused, EventPlaybackResumed, EventPlaybackStopped,
		EventSignedIn, EventSignInRefused, EventProfileAdded, EventProfileRemoved,
		EventLibraryAdded, EventLibraryRemoved, EventLibraryScanned, EventTitlesAdded, EventScanProgress,
		EventTaskStarted, EventTaskFinished, EventTaskFailed, EventBackupMade,
		EventJobStarted, EventJobFinished, EventJobFailed, EventJobDead,
	}
}

// Logged reports whether the activity log keeps an event of this kind: what an admin reads
// later, as Jellyfin's log keeps sign-ins, plays and failed tasks, and not the steps between.
func (k EventKind) Logged() bool {
	switch k {
	case EventPlaybackStarted, EventPlaybackStopped, EventSignedIn, EventSignInRefused,
		EventProfileAdded, EventProfileRemoved, EventLibraryAdded, EventLibraryRemoved,
		EventLibraryScanned, EventTitlesAdded, EventTaskFailed, EventBackupMade, EventJobDead:
		return true
	case EventPlaybackPaused, EventPlaybackResumed, EventScanProgress, EventTaskStarted,
		EventTaskFinished, EventJobStarted, EventJobFinished, EventJobFailed:
		return false
	}
	return false
}

// LoggedEventKinds are the kinds the activity log keeps.
func LoggedEventKinds() []EventKind {
	return slices.DeleteFunc(EventKinds(), func(k EventKind) bool { return !k.Logged() })
}

// Event is one thing that happened, and the profile, title and library it was about where there
// is one. ID is its activity entry's, for a kind the log keeps.
type Event struct {
	ID      uuid.UUID
	Kind    EventKind
	At      time.Time
	Profile uuid.UUID
	Item    uuid.UUID
	Library uuid.UUID
	Details map[string]any
}

// ScanPhase is what a library's scan is doing.
type ScanPhase string

const (
	// ScanReading reads the library's folders, probing what is new.
	ScanReading ScanPhase = "reading"
	// ScanRemoving forgets what is no longer there.
	ScanRemoving ScanPhase = "removing"
)

// ScanProgress is how far a scan has got: folders done of those found so far, which grows as it
// reads.
type ScanProgress struct {
	Library uuid.UUID
	Phase   ScanPhase
	Done    int
	Known   int
}
