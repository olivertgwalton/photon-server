package words

import (
	"cmp"
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

// Event is what happened, as a sentence for the activity log: "Ada started Heat".
func (w Words) Event(e domain.Event, names Names) string {
	profile := cmp.Or(name(names.Profile, e.Profile), "Someone")
	library := cmp.Or(name(names.Library, e.Library), "A library")
	play, _ := e.Details.(domain.PlaybackDetails)
	played, by := playedTitle(play.Playback.Title), cmp.Or(play.Playback.Profile.Name, profile)
	signIn, _ := e.Details.(domain.SignInDetails)
	named, _ := e.Details.(domain.NameDetails)
	task, _ := e.Details.(domain.TaskDetails)
	job, _ := e.Details.(domain.JobDetails)
	switch e.Kind {
	case domain.EventPlaybackStarted:
		return by + " started " + played
	case domain.EventPlaybackPaused:
		return by + " paused " + played
	case domain.EventPlaybackResumed:
		return by + " resumed " + played
	case domain.EventPlaybackStopped:
		if play.Reach == domain.ReachEnd {
			return by + " finished " + played
		}
		return by + " stopped " + played
	case domain.EventSignedIn:
		return fmt.Sprintf("%s signed in on %s (%s)", signIn.Name, signIn.Device, signIn.Client)
	case domain.EventSignInRefused:
		return fmt.Sprintf("A sign-in as %s from %s was refused", signIn.Name, signIn.Address)
	case domain.EventProfileAdded:
		return "Profile " + named.Name + " was added"
	case domain.EventProfileRemoved:
		return "Profile " + named.Name + " was removed"
	case domain.EventLibraryAdded:
		return "Library " + named.Name + " was added"
	case domain.EventLibraryRemoved:
		return "Library " + named.Name + " was removed"
	case domain.EventLibraryScanned:
		d, _ := e.Details.(domain.ScannedDetails)
		return fmt.Sprintf("%s was scanned: %d folders, %d read", library, d.Folders, d.Probed)
	case domain.EventTitlesAdded:
		d, _ := e.Details.(domain.TitlesAddedDetails)
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
		return w.Task(task.Task).Name + " started"
	case domain.EventTaskFinished:
		return w.Task(task.Task).Name + " finished"
	case domain.EventTaskFailed:
		return w.Task(task.Task).Name + " failed: " + task.Error
	case domain.EventBackupMade:
		d, _ := e.Details.(domain.BackupDetails)
		return "The database was backed up to " + d.File
	case domain.EventJobStarted:
		return w.JobKind(job.JobKind) + " started"
	case domain.EventJobFinished:
		return w.JobKind(job.JobKind) + " finished"
	case domain.EventJobFailed:
		return w.JobKind(job.JobKind) + " failed and will be tried again: " + job.Error
	case domain.EventJobDead:
		return fmt.Sprintf("%s gave up after %d tries: %s", w.JobKind(job.JobKind), job.Attempt, job.Error)
	case domain.EventJobsProgress:
		d, _ := e.Details.(domain.BacklogDetails)
		return fmt.Sprintf("%s: %d left", w.JobKind(d.JobKind), d.Left)
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
		d, _ := e.Details.(domain.RestoreDetails)
		return "The database is being restored from " + d.Dump
	}
	return string(e.Kind)
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
