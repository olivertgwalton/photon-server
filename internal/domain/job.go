package domain

type JobKind string

const (
	JobKeyframes JobKind = "keyframes"
	// JobIdentify matches a film or show to a metadata provider.
	JobIdentify JobKind = "identify"
)

func JobKinds() []JobKind {
	return []JobKind{JobKeyframes, JobIdentify}
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
