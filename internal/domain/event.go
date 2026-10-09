package domain

import (
	"encoding/json"
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
	// EventJobsProgress is how far a kind's backlog has got (a Backlog); it says so at most every
	// backlogProgressEvery, and once more when none is left.
	EventJobsProgress EventKind = "jobs.progress"
	// EventWebhookTest is sent to one webhook when an admin asks, and to no one else.
	EventWebhookTest EventKind = "webhook.test"
	// EventMaintenanceChanged is the maintenance window, or when work waits for it, changed by an
	// admin; every node keeps to it from then, as Plex applies its settings at once.
	EventMaintenanceChanged EventKind = "maintenance.changed"
	// EventNetworkChanged is whether the port answers HTTPS, or its certificate, changed by an
	// admin; every node serves it from then.
	EventNetworkChanged EventKind = "network.changed"
	// EventStorageChanged is where artwork and previews are kept, changed by an admin; every node
	// keeps them there from then.
	EventStorageChanged EventKind = "storage.changed"
	// EventNodesChanged is what an admin sets of a node, changed; the node takes it up from then.
	EventNodesChanged EventKind = "nodes.changed"
	// EventServerChanged is the server's name, or what its metadata is asked in, changed by an
	// admin; every node goes by it from then.
	EventServerChanged EventKind = "server.changed"
	// EventRestoreStarted is a restore asked for: every node stops until it is done, so every
	// client is told, to say the server will be back.
	EventRestoreStarted EventKind = "restore.started"
	// EventTrackerChanged is a profile's account on a tracker linked, or the code it was entering
	// ended unentered, told to the profile so a page showing the code shows what came of it.
	EventTrackerChanged EventKind = "tracker.changed"
)

func EventKinds() []EventKind {
	return []EventKind{
		EventPlaybackStarted, EventPlaybackPaused, EventPlaybackResumed, EventPlaybackStopped,
		EventSignedIn, EventSignInRefused, EventProfileAdded, EventProfileRemoved,
		EventLibraryAdded, EventLibraryRemoved, EventLibraryScanned, EventLibraryChanged, EventTitleUpdated,
		EventUserDataChanged, EventTitlesAdded, EventScanProgress, EventTaskStarted, EventTaskFinished, EventTaskFailed, EventBackupMade,
		EventJobStarted, EventJobFinished, EventJobFailed, EventJobDead, EventJobsProgress, EventWebhookTest,
		EventMaintenanceChanged, EventNetworkChanged, EventStorageChanged, EventNodesChanged, EventServerChanged,
		EventRestoreStarted, EventTrackerChanged,
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
		EventJobFinished, EventJobFailed, EventJobsProgress, EventWebhookTest, EventMaintenanceChanged,
		EventNetworkChanged, EventStorageChanged, EventNodesChanged, EventServerChanged, EventRestoreStarted,
		EventTrackerChanged:
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
		EventJobDead, EventJobsProgress, EventWebhookTest, EventMaintenanceChanged, EventNetworkChanged,
		EventStorageChanged, EventNodesChanged, EventServerChanged, EventRestoreStarted, EventTrackerChanged:
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
	Details EventDetails
}

// UnmarshalJSON reads an event told by another node, its details as its kind has them.
func (e *Event) UnmarshalJSON(b []byte) error {
	type plain Event
	var read struct {
		plain
		Details json.RawMessage
	}
	if err := json.Unmarshal(b, &read); err != nil {
		return err
	}
	*e = Event(read.plain)
	if string(read.Details) == "null" {
		return nil
	}
	var err error
	e.Details, err = DetailsOf(e.Kind, read.Details)
	return err
}

// EventDetails is what an event says beyond the profile, title and library it was about: one of
// the types below, each written as a JSON object, and none, written {}, for a kind that says no
// more. Their fields are in the order of their keys, as the maps they replace were written.
type EventDetails interface{ eventDetails() }

// DetailsOf reads the details of an event of kind k from JSON.
func DetailsOf(k EventKind, raw []byte) (EventDetails, error) {
	switch k {
	case EventPlaybackStarted, EventPlaybackPaused, EventPlaybackResumed, EventPlaybackStopped:
		return detailsOf[PlaybackDetails](raw)
	case EventSignedIn, EventSignInRefused:
		return detailsOf[SignInDetails](raw)
	case EventProfileAdded, EventProfileRemoved, EventLibraryAdded, EventLibraryRemoved:
		return detailsOf[NameDetails](raw)
	case EventLibraryScanned:
		return detailsOf[ScannedDetails](raw)
	case EventLibraryChanged:
		return detailsOf[LibraryChangedDetails](raw)
	case EventUserDataChanged:
		return detailsOf[UserDataDetails](raw)
	case EventTitlesAdded:
		return detailsOf[TitlesAddedDetails](raw)
	case EventScanProgress:
		return detailsOf[ScanProgressDetails](raw)
	case EventTaskStarted, EventTaskFinished, EventTaskFailed:
		return detailsOf[TaskDetails](raw)
	case EventBackupMade:
		return detailsOf[BackupDetails](raw)
	case EventRestoreStarted:
		return detailsOf[RestoreDetails](raw)
	case EventJobStarted, EventJobFinished, EventJobFailed, EventJobDead:
		return detailsOf[JobDetails](raw)
	case EventJobsProgress:
		return detailsOf[BacklogDetails](raw)
	case EventTrackerChanged:
		return detailsOf[TrackerDetails](raw)
	case EventTitleUpdated, EventWebhookTest, EventMaintenanceChanged, EventNetworkChanged,
		EventStorageChanged, EventNodesChanged, EventServerChanged:
	}
	return nil, nil
}

func detailsOf[T EventDetails](raw []byte) (EventDetails, error) {
	var d T
	err := json.Unmarshal(raw, &d)
	return d, err
}

// NoDetails is the details of an event that says no more.
type NoDetails struct{}

type PlaybackDetails struct {
	Playback NowPlaying `json:"playback"`
	// Reach is how far a stopped playback got.
	Reach Reach `json:"reach,omitzero"`
	// StoppedBy is what ended a stopped playback.
	StoppedBy StoppedBy `json:"stopped_by,omitzero"`
}

// PlaybackClosedDetails is what a profile's own player is told of its playback stopped.
type PlaybackClosedDetails struct {
	PlaybackID uuid.UUID `json:"playback_id"`
}

// SignInDetails is who tried to sign in, from where and on what; never the password.
type SignInDetails struct {
	Address string `json:"address"`
	Client  string `json:"client"`
	Device  string `json:"device"`
	Name    string `json:"name"`
}

// NameDetails is the name of a profile or library added or removed.
type NameDetails struct {
	Name string `json:"name"`
}

type ScannedDetails struct {
	Folders   int `json:"folders"`
	LeftOut   int `json:"left_out"`
	Probed    int `json:"probed"`
	Unchanged int `json:"unchanged"`
}

// LibraryChangedDetails are the ids under each TitleChange, every one present.
type LibraryChangedDetails map[TitleChange][]uuid.UUID

// UserDataDetails is the playlist changed; a title's state changed is the event's Item, told to
// its profile as the titles listed with it.
type UserDataDetails struct {
	PlaylistID uuid.UUID   `json:"playlist_id,omitzero"`
	TitleIDs   []uuid.UUID `json:"title_ids,omitzero"`
}

type TitlesAddedDetails struct {
	Titles int64 `json:"titles"`
}

type ScanProgressDetails struct {
	Done   int       `json:"done"`
	Folder string    `json:"folder"`
	Known  int       `json:"known"`
	Phase  ScanPhase `json:"phase"`
}

// TaskDetails is the task, and once it ends how it did, with the error it failed with.
type TaskDetails struct {
	Error  string     `json:"error,omitzero"`
	Result TaskResult `json:"result,omitzero"`
	Task   TaskKey    `json:"task"`
}

type BackupDetails struct {
	File string `json:"file"`
}

type RestoreDetails struct {
	Dump string `json:"dump"`
}

// JobDetails is the job, with the error an attempt failed with.
type JobDetails struct {
	Attempt int       `json:"attempt"`
	Error   string    `json:"error,omitzero"`
	JobID   int64     `json:"job_id"`
	JobKind JobKind   `json:"job_kind"`
	Subject uuid.UUID `json:"subject"`
}

// BacklogDetails is how far a kind's backlog has got.
type BacklogDetails struct {
	Done    int     `json:"done"`
	JobKind JobKind `json:"job_kind"`
	Left    int     `json:"left"`
}

// TrackerDetails is the tracker a profile's account is on.
type TrackerDetails struct {
	Tracker Tracker `json:"tracker"`
}

func (NoDetails) eventDetails()             {}
func (PlaybackDetails) eventDetails()       {}
func (PlaybackClosedDetails) eventDetails() {}
func (SignInDetails) eventDetails()         {}
func (NameDetails) eventDetails()           {}
func (ScannedDetails) eventDetails()        {}
func (LibraryChangedDetails) eventDetails() {}
func (UserDataDetails) eventDetails()       {}
func (TitlesAddedDetails) eventDetails()    {}
func (ScanProgressDetails) eventDetails()   {}
func (TaskDetails) eventDetails()           {}
func (BackupDetails) eventDetails()         {}
func (RestoreDetails) eventDetails()        {}
func (JobDetails) eventDetails()            {}
func (BacklogDetails) eventDetails()        {}
func (TrackerDetails) eventDetails()        {}

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
// reads, and the folder it read last, under the library's root ("" for the root itself).
type ScanProgress struct {
	Library uuid.UUID
	Phase   ScanPhase
	Done    int
	Known   int
	Folder  string
}
