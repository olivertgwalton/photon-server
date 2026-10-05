package domain

type TaskKey string

const (
	TaskScanLibraries TaskKey = "scan_libraries"
	TaskSweepJobs     TaskKey = "sweep_jobs"
)

func TaskKeys() []TaskKey {
	return []TaskKey{TaskScanLibraries, TaskSweepJobs}
}

type TaskResult string

const (
	TaskSucceeded TaskResult = "succeeded"
	TaskFailed    TaskResult = "failed"
	TaskCancelled TaskResult = "cancelled"
)

func TaskResults() []TaskResult {
	return []TaskResult{TaskSucceeded, TaskFailed, TaskCancelled}
}
