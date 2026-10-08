package words

import (
	"cmp"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// Task is a scheduled task's name, and what it does in a sentence.
func (w Words) Task(k domain.TaskKey) Described {
	return cmp.Or(tasks[k], Described{Name: string(k)})
}

var tasks = map[domain.TaskKey]Described{
	domain.TaskScanLibraries:      {"Scan libraries", "Reads every library's folders for what is new, changed or gone."},
	domain.TaskSweepJobs:          {"Requeue stalled jobs", "Puts back the jobs a node stopped working on."},
	domain.TaskBackupDatabase:     {"Back up the database", "Dumps the database for pg_restore, keeping the newest few."},
	domain.TaskRefreshMetadata:    {"Refresh metadata", "Asks the providers again about titles whose libraries say it is time."},
	domain.TaskSweepArtwork:       {"Clear old artwork", "Removes replaced pictures from the cache, takes the blur drawn while each loads where it has none, and fetches the pictures titles show that the cache lacks."},
	domain.TaskDetectMarkers:      {"Detect intros and credits", "Compares the sound of each season's episodes to find what they share, and finds where each film's picture goes dark for its credits."},
	domain.TaskBackfillPreviews:   {"Make previews", "Makes the chapter images and seek previews libraries ask for, and clears unused ones."},
	domain.TaskSweepDownloads:     {"Clear old downloads", "Forgets downloads kept past their time, and conversions nothing needs."},
	domain.TaskPruneActivity:      {"Prune the activity log", "Forgets activity older than 30 days."},
	domain.TaskSyncLists:          {"Sync list collections", "Reads each list collection's TMDB or MDBList list again and keeps the titles of it the library has."},
	domain.TaskRefreshCollections: {"Refresh smart collections", "Finds what each smart collection's filters hold again, catching what was matched or edited since."},
	domain.TaskFetchSubtitles:     {"Download missing subtitles", "Fetches subtitles in the languages libraries name for copies with none in them."},
}

// Names are the names of what an event refers to by id: its profile and its library.
type Names interface {
	Profile(id uuid.UUID) string
	Library(id uuid.UUID) string
}

// said is what an event's details may say, read alike whether the event was raised a moment ago or
// read back from the activity log.
type said struct {
	Name     string               `json:"name"`
	Device   string               `json:"device"`
	Client   string               `json:"client"`
	Address  string               `json:"address"`
	Error    string               `json:"error"`
	File     string               `json:"file"`
	Dump     string               `json:"dump"`
	Reach    domain.Reach         `json:"reach"`
	Task     domain.TaskKey       `json:"task"`
	JobKind  domain.JobKind       `json:"job_kind"`
	Attempt  int                  `json:"attempt"`
	Left     int                  `json:"left"`
	Folders  int                  `json:"folders"`
	Probed   int                  `json:"probed"`
	Titles   int                  `json:"titles"`
	Playback *domain.PlaybackCard `json:"playback"`
}

// Event is what happened, as a sentence for the activity log: "Ada started Heat".
func (w Words) Event(e domain.Event, names Names) string {
	d, err := saidOf(e.Details)
	if err != nil {
		return string(e.Kind)
	}
	profile := cmp.Or(name(names.Profile, e.Profile), "Someone")
	library := cmp.Or(name(names.Library, e.Library), "A library")
	played, by := "a title", profile
	if p := d.Playback; p != nil {
		played, by = playedTitle(p.Title), cmp.Or(p.Profile.Name, profile)
	}
	task := w.Task(d.Task).Name
	job := w.Name(d.JobKind)
	switch e.Kind {
	case domain.EventPlaybackStarted:
		return by + " started " + played
	case domain.EventPlaybackPaused:
		return by + " paused " + played
	case domain.EventPlaybackResumed:
		return by + " resumed " + played
	case domain.EventPlaybackStopped:
		if d.Reach == domain.ReachEnd {
			return by + " finished " + played
		}
		return by + " stopped " + played
	case domain.EventSignedIn:
		return fmt.Sprintf("%s signed in on %s (%s)", d.Name, d.Device, d.Client)
	case domain.EventSignInRefused:
		return fmt.Sprintf("A sign-in as %s from %s was refused", d.Name, d.Address)
	case domain.EventProfileAdded:
		return "Profile " + d.Name + " was added"
	case domain.EventProfileRemoved:
		return "Profile " + d.Name + " was removed"
	case domain.EventLibraryAdded:
		return "Library " + d.Name + " was added"
	case domain.EventLibraryRemoved:
		return "Library " + d.Name + " was removed"
	case domain.EventLibraryScanned:
		return fmt.Sprintf("%s was scanned: %d folders, %d read", library, d.Folders, d.Probed)
	case domain.EventTitlesAdded:
		if d.Titles == 1 {
			return "1 title was added to " + library
		}
		return fmt.Sprintf("%d titles were added to %s", d.Titles, library)
	case domain.EventScanProgress:
		return library + " is scanning"
	case domain.EventLibraryChanged:
		return library + " changed"
	case domain.EventTitleUpdated:
		return "A title was described again"
	case domain.EventUserDataChanged:
		return profile + "'s watching changed"
	case domain.EventTaskStarted:
		return task + " started"
	case domain.EventTaskFinished:
		return task + " finished"
	case domain.EventTaskFailed:
		return task + " failed: " + d.Error
	case domain.EventBackupMade:
		return "The database was backed up to " + d.File
	case domain.EventJobStarted:
		return job + " started"
	case domain.EventJobFinished:
		return job + " finished"
	case domain.EventJobFailed:
		return job + " failed and will be tried again: " + d.Error
	case domain.EventJobDead:
		return fmt.Sprintf("%s gave up after %d tries: %s", job, d.Attempt, d.Error)
	case domain.EventJobsProgress:
		return fmt.Sprintf("%s: %d left", job, d.Left)
	case domain.EventWebhookTest:
		return "A webhook test was sent"
	case domain.EventMaintenanceChanged:
		return "The maintenance window was changed"
	case domain.EventNetworkChanged:
		return "Secure connections were changed"
	case domain.EventStorageChanged:
		return "Where artwork and previews are kept was changed"
	case domain.EventNodesChanged:
		return "What a server node does was changed"
	case domain.EventRestoreStarted:
		return "The database is being restored from " + d.Dump
	}
	return string(e.Kind)
}

// saidOf reads details through JSON, as a playback card raised a moment ago is a struct and one
// read back from the activity log is a map.
func saidOf(details map[string]any) (said, error) {
	var d said
	raw, err := json.Marshal(details)
	if err != nil {
		return d, err
	}
	err = json.Unmarshal(raw, &d)
	return d, err
}

func name(of func(uuid.UUID) string, id uuid.UUID) string {
	if id == (uuid.UUID{}) {
		return ""
	}
	return of(id)
}

// playedTitle is a played title as one line: an episode by its show and its place in it,
// "Small Show S1 E2 · Second"; anything else by its own title.
func playedTitle(t domain.PlaybackTitle) string {
	if t.Kind != domain.ItemEpisode || t.Show == "" {
		return t.Title
	}
	at := ""
	if t.EpisodeNumber != nil {
		at = "E" + strconv.Itoa(*t.EpisodeNumber)
		if t.EpisodeEnd != nil && *t.EpisodeEnd > *t.EpisodeNumber {
			at += "–E" + strconv.Itoa(*t.EpisodeEnd)
		}
		if t.SeasonNumber != nil {
			at = "S" + strconv.Itoa(*t.SeasonNumber) + " " + at
		}
	}
	return strings.Join(strings.Fields(t.Show+" "+at), " ") + " · " + t.Title
}
