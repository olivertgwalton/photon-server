package domain

type JobKind string

const JobKeyframes JobKind = "keyframes"

func JobKinds() []JobKind {
	return []JobKind{JobKeyframes}
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
