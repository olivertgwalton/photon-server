package domain

import (
	"errors"
	"uuid"
)

type JobKind string

const (
	// JobKeyframes reads a part's keyframes from its container's own index.
	JobKeyframes JobKind = "keyframes"
	// JobKeyframeWalk walks a part with no index through for its keyframes, under KeyframesFull.
	JobKeyframeWalk JobKind = "keyframe_walk"
	// JobIdentify matches a film or show to a metadata provider.
	JobIdentify JobKind = "identify"
	// JobScanLibrary reads a library's folders again; one job per library at a time.
	JobScanLibrary JobKind = "scan_library"
	// JobMarkers finds the intro and credits a season's episodes share by their sound.
	JobMarkers JobKind = "markers"
	// JobPreviews makes a part's chapter images and trickplay sheets, as its library asks.
	JobPreviews JobKind = "previews"
	// JobConvert makes a part smaller for downloading; one job per conversion.
	JobConvert JobKind = "convert"
	// JobDeliverWebhook sends one event to one webhook.
	JobDeliverWebhook JobKind = "deliver_webhook"
	// JobTheme fetches a film's or show's theme tune from the YouTube link ThemerrDB lists.
	JobTheme JobKind = "theme"
)

func JobKinds() []JobKind {
	return []JobKind{JobKeyframes, JobKeyframeWalk, JobIdentify, JobScanLibrary, JobMarkers, JobPreviews, JobConvert, JobDeliverWebhook, JobTheme}
}

type JobState string

const (
	JobQueued  JobState = "queued"
	JobRunning JobState = "running"
	// JobRerun is a running job whose subject changed again after it was claimed: when it ends
	// it is queued again, not removed.
	JobRerun JobState = "rerun"
	// JobDead is a job that failed every attempt; it waits for its subject to change.
	JobDead JobState = "dead"
)

func JobStates() []JobState {
	return []JobState{JobQueued, JobRunning, JobRerun, JobDead}
}

// Backlog is how far the jobs of a kind have got: left to run, queued or running, and done since
// the kind last had none left, so its total grows as more are queued, as Plex's activity does.
// JobDue is when a job is meant to run, decided as it is queued: as soon as there is room, as what
// an admin asks for is and what a scan finds where its timing says so; or in the maintenance
// window, as the work the window's own backfill queues is, Plex's "existing items during the
// maintenance period".
type JobDue string

const (
	JobDueNow    JobDue = "now"
	JobDueWindow JobDue = "window"
)

func JobDues() []JobDue {
	return []JobDue{JobDueNow, JobDueWindow}
}

type Backlog struct {
	Kind JobKind
	Left int
	Done int
}

type Job struct {
	ID       int64
	Kind     JobKind
	Subject  uuid.UUID
	Attempts int
	Due      JobDue
}

// About is the title, season or library a job is about, where its subject is one; a part's
// keyframes or previews, a download's conversion and a webhook's delivery are neither.
func (j Job) About() (item, library uuid.UUID) {
	switch j.Kind {
	case JobIdentify, JobMarkers, JobTheme:
		return j.Subject, uuid.UUID{}
	case JobScanLibrary:
		return uuid.UUID{}, j.Subject
	case JobKeyframes, JobKeyframeWalk, JobPreviews, JobConvert, JobDeliverWebhook:
	}
	return uuid.UUID{}, uuid.UUID{}
}

// ErrLeaseLost is a node's answer for a job it no longer holds: its lease ran out and the job was
// queued again, and may be running elsewhere.
var ErrLeaseLost = errors.New("the job's lease ran out and it was queued again")
