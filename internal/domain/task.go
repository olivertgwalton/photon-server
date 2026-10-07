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
	// TaskSweepArtwork clears replaced pictures from the cache and fetches into it those titles
	// show first that it lacks.
	TaskSweepArtwork TaskKey = "sweep_artwork"
	// TaskDetectMarkers queues the seasons with episodes whose sound has not been compared.
	TaskDetectMarkers TaskKey = "detect_markers"
	// TaskBackfillPreviews queues the parts whose previews are not what their library asks for,
	// and clears previews no part has any more or whose file has long been missing.
	TaskBackfillPreviews TaskKey = "backfill_previews"
	// TaskSweepDownloads forgets downloads kept past their retention, and conversions no download
	// needs.
	TaskSweepDownloads TaskKey = "sweep_downloads"
	// TaskPruneActivity forgets activity older than the log keeps.
	TaskPruneActivity TaskKey = "prune_activity"
	// TaskRefreshCollections finds the titles of every smart collection again.
	TaskRefreshCollections TaskKey = "refresh_collections"
	// TaskSyncLists reads every list collection's list again, as Kometa's daily run.
	TaskSyncLists TaskKey = "sync_lists"
	// TaskFetchSubtitles fetches the subtitles copies lack in the languages their libraries name.
	TaskFetchSubtitles TaskKey = "fetch_subtitles"
)

func TaskKeys() []TaskKey {
	return []TaskKey{TaskScanLibraries, TaskSweepJobs, TaskBackupDatabase, TaskRefreshMetadata, TaskSweepArtwork, TaskDetectMarkers, TaskBackfillPreviews, TaskSweepDownloads, TaskPruneActivity, TaskRefreshCollections, TaskSyncLists, TaskFetchSubtitles}
}

// Jobs are the kinds of job the task queues: the work it starts that outlasts its run, and that
// stopping it stops. A task that does all it does in its run queues none.
func (k TaskKey) Jobs() []JobKind {
	switch k {
	case TaskScanLibraries:
		return []JobKind{JobScanLibrary}
	case TaskRefreshMetadata:
		return []JobKind{JobIdentify}
	case TaskDetectMarkers:
		return []JobKind{JobMarkers}
	case TaskBackfillPreviews:
		return []JobKind{JobPreviews}
	case TaskSweepJobs, TaskBackupDatabase, TaskSweepArtwork, TaskSweepDownloads, TaskPruneActivity, TaskRefreshCollections, TaskSyncLists, TaskFetchSubtitles:
	}
	return nil
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
