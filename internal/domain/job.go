package domain

type JobKind string

const (
	JobKeyframes JobKind = "keyframes"
	// JobIdentify matches a film or show to a metadata provider.
	JobIdentify JobKind = "identify"
	// JobScanLibrary reads a library's folders again; one job per library at a time.
	JobScanLibrary JobKind = "scan_library"
	// JobMarkers finds the intro and credits a season's episodes share by their sound.
	JobMarkers JobKind = "markers"
)

func JobKinds() []JobKind {
	return []JobKind{JobKeyframes, JobIdentify, JobScanLibrary, JobMarkers}
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
