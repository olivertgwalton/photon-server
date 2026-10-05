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
	// JobDead is a job that failed every attempt; it waits for its subject to change.
	JobDead JobState = "dead"
)

func JobStates() []JobState {
	return []JobState{JobQueued, JobRunning, JobDead}
}
