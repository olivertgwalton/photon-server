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
	// EventLibraryChanged is a library's titles added, changed and removed, gathered over a
	// moment, as Jellyfin's LibraryChanged is: its details hold the ids under each TitleChange.
	EventLibraryChanged EventKind = "library.changed"
	// EventTitleUpdated is a title described again: matched, edited, or given another picture.
	EventTitleUpdated EventKind = "title.updated"
	// EventUserDataChanged is a profile's own state changed: where it got to in a title, what it
	// has watched or favoured, or one of its playlists, as Jellyfin's UserDataChanged.
	EventUserDataChanged EventKind = "userdata.changed"
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
	// EventWebhookTest is sent to one webhook when an admin asks, and to no one else.
	EventWebhookTest EventKind = "webhook.test"
)

func EventKinds() []EventKind {
	return []EventKind{
		EventPlaybackStarted, EventPlaybackPaused, EventPlaybackResumed, EventPlaybackStopped,
		EventSignedIn, EventSignInRefused, EventProfileAdded, EventProfileRemoved,
		EventLibraryAdded, EventLibraryRemoved, EventLibraryScanned, EventLibraryChanged, EventTitleUpdated,
		EventUserDataChanged, EventTitlesAdded, EventScanProgress, EventTaskStarted, EventTaskFinished, EventTaskFailed, EventBackupMade,
		EventJobStarted, EventJobFinished, EventJobFailed, EventJobDead, EventWebhookTest,
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
	case EventPlaybackPaused, EventPlaybackResumed, EventLibraryChanged, EventTitleUpdated,
		EventUserDataChanged, EventScanProgress, EventTaskStarted, EventTaskFinished, EventJobStarted,
		EventJobFinished, EventJobFailed, EventWebhookTest:
		return false
	}
	return false
}

// Hookable reports whether a webhook may ask for events of this kind: what happens to people and
// libraries, as Plex's webhooks tell plays and new titles, and not the server's own work. A job is
// how a webhook is called, so one that told jobs would call itself without end.
func (k EventKind) Hookable() bool {
	switch k {
	case EventPlaybackStarted, EventPlaybackPaused, EventPlaybackResumed, EventPlaybackStopped,
		EventSignedIn, EventSignInRefused, EventProfileAdded, EventProfileRemoved,
		EventLibraryAdded, EventLibraryRemoved, EventLibraryScanned, EventTitlesAdded,
		EventTaskFailed, EventBackupMade:
		return true
	case EventLibraryChanged, EventTitleUpdated, EventUserDataChanged, EventScanProgress,
		EventTaskStarted, EventTaskFinished, EventJobStarted, EventJobFinished, EventJobFailed,
		EventJobDead, EventWebhookTest:
		return false
	}
	return false
}

// HookableEventKinds are the kinds a webhook may ask for.
func HookableEventKinds() []EventKind {
	return slices.DeleteFunc(EventKinds(), func(k EventKind) bool { return !k.Hookable() })
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

// TitleChange is what happened to a library's titles.
type TitleChange string

const (
	TitleAdded   TitleChange = "added"
	TitleUpdated TitleChange = "updated"
	TitleRemoved TitleChange = "removed"
)

// TitleChanges are the changes, the one that says most of a title first: one removed is gone,
// whatever else happened to it, and one added is new, however it changed since.
func TitleChanges() []TitleChange {
	return []TitleChange{TitleRemoved, TitleAdded, TitleUpdated}
}

// ScanPhase is what a library's scan is doing.
type ScanPhase string

const (
	// ScanReading reads the library's folders, probing what is new.
	ScanReading ScanPhase = "reading"
	// ScanRemoving forgets what is no longer there.
	ScanRemoving ScanPhase = "removing"
)

func ScanPhases() []ScanPhase {
	return []ScanPhase{ScanReading, ScanRemoving}
}

// ScanProgress is how far a scan has got: folders done of those found so far, which grows as it
// reads.
type ScanProgress struct {
	Library uuid.UUID
	Phase   ScanPhase
	Done    int
	Known   int
}
