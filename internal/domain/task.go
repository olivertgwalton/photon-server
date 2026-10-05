package domain

import "time"

type TaskKey string

const (
	TaskScanLibraries TaskKey = "scan_libraries"
	TaskSweepJobs     TaskKey = "sweep_jobs"
	// TaskBackupDatabase dumps the database for pg_restore.
	TaskBackupDatabase TaskKey = "backup_database"
	// TaskRefreshMetadata matches again the titles whose libraries say it is time.
	TaskRefreshMetadata TaskKey = "refresh_metadata"
)

func TaskKeys() []TaskKey {
	return []TaskKey{TaskScanLibraries, TaskSweepJobs, TaskBackupDatabase, TaskRefreshMetadata}
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

// TaskState is what is known of a task's runs: when the last started and finished, how it ended,
// and when an admin last asked for it to run now.
type TaskState struct {
	Started   time.Time
	Finished  time.Time
	Result    TaskResult
	Error     string
	Requested time.Time
}
