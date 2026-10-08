package words

import (
	"testing"
	"uuid"

	"golang.org/x/text/language"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

type known map[uuid.UUID]string

func (n known) Profile(id uuid.UUID) string { return n[id] }
func (n known) Library(id uuid.UUID) string { return n[id] }

// An event reads as a sentence, whether raised a moment ago with Go values or read back from the
// activity log as JSON.
func TestAnEventReadsAsASentence(t *testing.T) {
	w := In(language.English)
	films := uuid.NewV7()
	known := known{films: "Films"}
	two, three, one := 2, 3, 1
	episode := domain.PlaybackCard{
		Profile: domain.PlaybackProfile{Name: "Ada"},
		Title:   domain.PlaybackTitle{Kind: domain.ItemEpisode, Title: "Second", Show: "Small Show", SeasonNumber: &one, EpisodeNumber: &two, EpisodeEnd: &three},
	}
	for _, c := range []struct {
		event domain.Event
		want  string
	}{
		{domain.Event{Kind: domain.EventPlaybackStarted, Details: domain.PlaybackDetails{Playback: domain.NowPlaying{PlaybackCard: episode}}}, "Ada started Small Show S1 E2–E3 · Second"},
		{domain.Event{Kind: domain.EventPlaybackStopped, Details: domain.PlaybackDetails{
			Playback: domain.NowPlaying{PlaybackCard: domain.PlaybackCard{
				Profile: domain.PlaybackProfile{Name: "Ada"}, Title: domain.PlaybackTitle{Kind: domain.ItemMovie, Title: "Heat"},
			}},
			Reach: domain.ReachEnd,
		}}, "Ada finished Heat"},
		{domain.Event{Kind: domain.EventLibraryScanned, Library: films, Details: domain.ScannedDetails{Folders: 12, Probed: 3}}, "Films was scanned: 12 folders, 3 read"},
		{domain.Event{Kind: domain.EventTitlesAdded, Library: films, Details: domain.TitlesAddedDetails{Titles: 1}}, "1 title was added to Films"},
		{domain.Event{Kind: domain.EventTaskFailed, Details: domain.TaskDetails{Task: domain.TaskBackupDatabase, Error: "disk full"}}, "Back up the database failed: disk full"},
		{domain.Event{Kind: domain.EventJobDead, Details: domain.JobDetails{JobKind: domain.JobPreviews, Attempt: 5, Error: "no ffmpeg"}}, "Make previews gave up after 5 tries: no ffmpeg"},
		{domain.Event{Kind: domain.EventLibraryChanged, Library: uuid.NewV7()}, "A library changed"},
	} {
		if got := w.Event(c.event, known); got != c.want {
			t.Errorf("%s: %q, want %q", c.event.Kind, got, c.want)
		}
	}
}

// A home row is headed by what it holds, a library's by the library, a collection's by its name.
func TestAHomeRowIsHeadedByWhatItHolds(t *testing.T) {
	w := In(language.English)
	for _, c := range []struct {
		kind                domain.HomeRow
		library, collection string
		want                string
	}{
		{domain.RowRecentFilms, "Films", "", "Recently Added in Films"},
		{domain.RowTopRatedUnwatched, "Shows", "", "Top Rated in Shows"},
		{domain.RowCollection, "", "Alien Anthology", "Alien Anthology"},
		{domain.RowContinueWatching, "", "", "Continue Watching"},
	} {
		if got := w.HomeRow(c.kind, c.library, c.collection); got != c.want {
			t.Errorf("HomeRow(%s, %q, %q) = %q, want %q", c.kind, c.library, c.collection, got, c.want)
		}
	}
}
