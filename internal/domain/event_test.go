package domain

import (
	"encoding/json"
	"testing"
	"uuid"

	"github.com/google/go-cmp/cmp"
)

// An event's details are written byte for byte as the maps before them were, so clients and the
// activity log read them as ever, and the log's rows read back as the details they were.
func TestEventDetailsAreWrittenAsTheyWere(t *testing.T) {
	id := uuid.NewV7()
	playing := NowPlaying{ID: id, Method: PlayDirect}
	for _, c := range []struct {
		kind    EventKind
		details EventDetails
		was     map[string]any
	}{
		{EventPlaybackStarted, PlaybackDetails{Playback: playing}, map[string]any{"playback": playing}},
		{EventPlaybackStopped, PlaybackDetails{Playback: playing, Reach: ReachEnd}, map[string]any{"playback": playing, "reach": ReachEnd}},
		{
			EventSignedIn,
			SignInDetails{Name: "Ada", Device: "TV", Client: "Photon", Address: "203.0.113.9"},
			map[string]any{"name": "Ada", "device": "TV", "client": "Photon", "address": "203.0.113.9"},
		},
		{EventLibraryRemoved, NameDetails{Name: "Films"}, map[string]any{"name": "Films"}},
		{
			EventLibraryScanned,
			ScannedDetails{Folders: 12, Probed: 3},
			map[string]any{"folders": 12, "unchanged": 0, "probed": 3, "left_out": 0},
		},
		{
			EventLibraryChanged,
			LibraryChangedDetails{TitleRemoved: {}, TitleAdded: {id}, TitleUpdated: {}},
			map[string]any{"removed": []uuid.UUID{}, "added": []uuid.UUID{id}, "updated": []uuid.UUID{}},
		},
		{EventUserDataChanged, UserDataDetails{PlaylistID: id}, map[string]any{"playlist_id": id}},
		{EventUserDataChanged, UserDataDetails{TitleIDs: []uuid.UUID{}}, map[string]any{"title_ids": []uuid.UUID{}}},
		{EventUserDataChanged, UserDataDetails{}, map[string]any{}},
		{EventTitlesAdded, TitlesAddedDetails{Titles: 2}, map[string]any{"titles": int64(2)}},
		{
			EventScanProgress,
			ScanProgressDetails{Phase: ScanReading, Known: 4},
			map[string]any{"phase": ScanReading, "done": 0, "known": 4, "folder": ""},
		},
		{EventTaskStarted, TaskDetails{Task: TaskSweepJobs}, map[string]any{"task": TaskSweepJobs}},
		{
			EventTaskFailed,
			TaskDetails{Task: TaskSweepJobs, Result: TaskFailed, Error: "gone"},
			map[string]any{"task": TaskSweepJobs, "result": TaskFailed, "error": "gone"},
		},
		{EventBackupMade, BackupDetails{File: "a.dump"}, map[string]any{"file": "a.dump"}},
		{EventRestoreStarted, RestoreDetails{Dump: "a.dump"}, map[string]any{"dump": "a.dump"}},
		{
			EventJobDead,
			JobDetails{JobID: 7, JobKind: JobPreviews, Subject: id, Attempt: 5, Error: "no ffmpeg"},
			map[string]any{"job_id": 7, "job_kind": JobPreviews, "subject": id, "attempt": 5, "error": "no ffmpeg"},
		},
		{EventJobsProgress, BacklogDetails{JobKind: JobPreviews}, map[string]any{"job_kind": JobPreviews, "left": 0, "done": 0}},
	} {
		was, err := json.Marshal(c.was)
		if err != nil {
			t.Fatal(err)
		}
		if got, err := json.Marshal(c.details); err != nil || string(got) != string(was) {
			t.Errorf("%s written as %s, %v; want %s", c.kind, got, err, was)
		}
		if got, err := DetailsOf(c.kind, was); err != nil || !cmp.Equal(got, c.details) {
			t.Errorf("%s read back as %+v, %v; want %+v", c.kind, got, err, c.details)
		}
	}
}
