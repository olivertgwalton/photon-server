package domain

type TaskKey string

const TaskScanLibraries TaskKey = "scan_libraries"

func TaskKeys() []TaskKey {
	return []TaskKey{TaskScanLibraries}
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
