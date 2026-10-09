//go:build integration

package analysis

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/library"
	"github.com/olivertgwalton/photon-server/internal/media"
	"github.com/olivertgwalton/photon-server/internal/store"
	"github.com/olivertgwalton/photon-server/internal/store/storetest"
)

// lost is a library of one season of three 44-minute episodes with sound, each with the chapters
// given, kept as markers says.
func lost(t *testing.T, markers domain.MarkerDetection, chapters ...domain.Chapter) (*store.Store, uuid.UUID) {
	t.Helper()
	log := slog.New(slog.DiscardHandler)
	url := storetest.FreshDatabase(t)
	if err := store.Migrate(t.Context(), url, log); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(t.Context(), url, log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	ctx := t.Context()
	root := t.TempDir()
	lib, err := st.AddLibrary(ctx, "TV", domain.LibraryShows, root)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetLibrary(ctx, lib.ID, store.LibraryChange{Markers: markers}); err != nil {
		t.Fatal(err)
	}
	var episodes []store.Episode
	for n := 1; n <= 3; n++ {
		rel := filepath.Join("Lost", "Season 1", "S01E0"+string(rune('0'+n))+".mkv")
		if err := os.MkdirAll(filepath.Join(root, filepath.Dir(rel)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, rel), nil, 0o600); err != nil {
			t.Fatal(err)
		}
		episodes = append(episodes, store.Episode{
			Season: 1, Episodes: []int{n}, Title: "Lost", Folder: "Lost/Season 1", ByNumber: true,
			Copies: []store.Copy{{ContentKey: []byte(rel), Parts: []store.Part{{RelPath: rel, Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{
				Duration: 44 * time.Minute, Streams: []domain.Stream{{Kind: domain.StreamAudio, Codec: "aac"}}, Chapters: chapters,
			}}}}},
		})
	}
	if _, err := st.SaveShowFolder(ctx, lib.ID, "Lost/Season 1", []byte("v1"), store.Show{Title: "Lost", Folder: "Lost"}, episodes, nil); err != nil {
		t.Fatal(err)
	}
	return st, lib.ID
}

func TestASeasonsSharedIntroIsFound(t *testing.T) {
	st, lib := lost(t, domain.MarkersAll)
	ctx := t.Context()
	jobs, err := st.ClaimJobs(ctx, []domain.JobKind{domain.JobMarkers}, nil, uuid.NewV7(), time.Minute, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 0 {
		t.Fatalf("claimed %v at once, want the season left to settle first", jobs)
	}
	if n, err := st.QueueSeasonMarkers(ctx, domain.JobDueWindow); err != nil || n != 0 {
		t.Errorf("the backfill queued %d (%v), want the season already queued", n, err)
	}

	// The first two episodes open with the theme; the third goes straight in. All three end
	// differently.
	theme := points(1, at(50*time.Second))
	asked := 0
	fake := func(_ context.Context, in media.Input, from, span time.Duration) ([]uint32, error) {
		asked++
		episode := uint64(in.File.Name()[len(in.File.Name())-5] - '0')
		rest := points(10*episode+uint64(from/time.Minute), at(span))
		if from == 0 && episode < 3 {
			return join(points(episode, at(30*time.Second)), theme, rest), nil
		}
		return rest, nil
	}
	season := seasonOf(t, st, lib)
	parts, err := st.SeasonParts(ctx, season)
	if err != nil || len(parts) != 3 {
		t.Fatalf("season parts %+v, %v", parts, err)
	}
	if err := Markers(st, library.Parts{Places: st}, fake, nil)(ctx, season); err != nil {
		t.Fatal(err)
	}
	for _, p := range parts {
		page, err := st.Title(ctx, uuid.UUID{}, p.Episode)
		if err != nil {
			t.Fatal(err)
		}
		got := page.Versions[0].Markers
		if page.EpisodeNumber != nil && *page.EpisodeNumber == 3 {
			if len(got) != 0 {
				t.Errorf("the episode without the theme: %+v, want no markers", got)
			}
			continue
		}
		if len(got) != 1 || got[0].Kind != domain.MarkerIntro || got[0].Source != domain.MarkerByFingerprint ||
			!near(got[0].StartMS, 30*time.Second) || !near(got[0].EndMS, 80*time.Second) {
			t.Errorf("episode %d: %+v, want the theme from 0:30 to 1:20", *page.EpisodeNumber, got)
		}
	}

	// Asked again with nothing new, the season is passed over.
	asked = 0
	if err := Markers(st, library.Parts{Places: st}, fake, nil)(ctx, season); err != nil || asked != 0 {
		t.Errorf("again: %d fingerprints taken, %v; want none", asked, err)
	}
	if n, err := st.QueueSeasonMarkers(ctx, domain.JobDueWindow); err != nil || n != 0 {
		t.Errorf("the backfill queued %d (%v), want nothing left to compare", n, err)
	}
}

// A library reading only chapters offers the markers they name, and queues nothing that reads
// its episodes' sound until it is set to compare it.
func TestALibraryOnChaptersReadsNoSound(t *testing.T) {
	st, lib := lost(t, domain.MarkersChapters,
		domain.Chapter{Start: 0, End: 30 * time.Second, Title: "Cold Open"},
		domain.Chapter{Start: 30 * time.Second, End: 80 * time.Second, Title: "Opening"},
		domain.Chapter{Start: 80 * time.Second, End: 44 * time.Minute, Title: "Episode"})
	ctx := t.Context()
	counts, _, err := st.JobQueue(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range counts {
		if c.Kind == domain.JobMarkers {
			t.Errorf("the scan queued %+v, want no comparison", c)
		}
	}
	if n, err := st.QueueSeasonMarkers(ctx, domain.JobDueWindow); err != nil || n != 0 {
		t.Errorf("the daily task queued %d (%v), want none", n, err)
	}
	season := seasonOf(t, st, lib)
	asked := 0
	fake := func(context.Context, media.Input, time.Duration, time.Duration) ([]uint32, error) {
		asked++
		return points(uint64(asked), at(time.Minute)), nil
	}
	if err := Markers(st, library.Parts{Places: st}, fake, nil)(ctx, season); err != nil || asked != 0 {
		t.Errorf("a comparison already queued took %d fingerprints (%v), want none", asked, err)
	}
	page, err := st.Title(ctx, uuid.UUID{}, season)
	if err != nil || len(page.Episodes) != 3 {
		t.Fatal(page.Episodes, err)
	}
	markers := func() []store.MarkerRef {
		ep, err := st.Title(ctx, uuid.UUID{}, page.Episodes[0].ID)
		if err != nil {
			t.Fatal(err)
		}
		return ep.Versions[0].Markers
	}
	opening := store.MarkerRef{Kind: domain.MarkerIntro, StartMS: 30_000, EndMS: 80_000, Source: domain.MarkerByChapter}
	if got := markers(); len(got) != 1 || got[0] != opening {
		t.Errorf("on chapters: %+v, want the opening its chapters name", got)
	}

	if err := st.SetLibrary(ctx, lib, store.LibraryChange{Markers: domain.MarkersOff}); err != nil {
		t.Fatal(err)
	}
	if got := markers(); len(got) != 0 {
		t.Errorf("off: %+v, want none", got)
	}

	if err := st.SetLibrary(ctx, lib, store.LibraryChange{Markers: domain.MarkersAll}); err != nil {
		t.Fatal(err)
	}
	if n, err := st.QueueSeasonMarkers(ctx, domain.JobDueWindow); err != nil || n != 1 {
		t.Errorf("set to compare sound, the daily task queued %d (%v), want the season", n, err)
	}
	if err := Markers(st, library.Parts{Places: st}, fake, nil)(ctx, season); err != nil || asked != 6 {
		t.Errorf("compared: %d fingerprints taken (%v), want each episode's start and end", asked, err)
	}
	if got := markers(); len(got) != 1 || got[0] != opening {
		t.Errorf("compared: %+v, want the chapters' opening still", got)
	}
}

func seasonOf(t *testing.T, st *store.Store, lib uuid.UUID) uuid.UUID {
	t.Helper()
	cards, _, err := st.Wall(t.Context(), []uuid.UUID{lib}, store.WallPage{Sort: domain.SortTitle, Limit: 1})
	if err != nil || len(cards) != 1 {
		t.Fatal(cards, err)
	}
	show, err := st.Title(t.Context(), uuid.UUID{}, cards[0].ID)
	if err != nil || len(show.Seasons) != 1 {
		t.Fatal(show.Seasons, err)
	}
	return show.Seasons[0].ID
}

// A film's credits are found by its picture, as a scan queues it, and shown on its title; a film
// already read is passed over, and the backfill finds nothing left to read.
func TestAFilmsCreditsAreFoundByItsPicture(t *testing.T) {
	log := slog.New(slog.DiscardHandler)
	url := storetest.FreshDatabase(t)
	if err := store.Migrate(t.Context(), url, log); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(t.Context(), url, log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	ctx := t.Context()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "Heat"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Heat", "Heat.mkv"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	lib, err := st.AddLibrary(ctx, "Films", domain.LibraryMovies, root)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetLibrary(ctx, lib.ID, store.LibraryChange{Markers: domain.MarkersAll}); err != nil {
		t.Fatal(err)
	}
	film := store.Film{Title: "Heat", Folder: "Heat", Copies: []store.Copy{{ContentKey: []byte("heat"), Parts: []store.Part{{
		RelPath: "Heat/Heat.mkv", Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{
			Duration: 2 * time.Hour, Streams: []domain.Stream{{Kind: domain.StreamVideo, Codec: "h264", Range: domain.RangeSDR}},
		},
	}}}}}
	if _, err := st.SaveFolder(ctx, lib.ID, "Heat", []byte("v1"), []store.Film{film}, nil); err != nil {
		t.Fatal(err)
	}
	jobs, err := st.ClaimJobs(ctx, []domain.JobKind{domain.JobMarkers}, nil, uuid.NewV7(), time.Minute, 1)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("claimed %v, %v; want the film's markers queued by its scan", jobs, err)
	}
	crawl := 2*time.Hour - 8*time.Minute
	read := 0
	fake := func(_ context.Context, _ media.Input, from time.Duration) ([]media.Shade, error) {
		read++
		return shadesOf(from, 2*time.Hour, 0, func(at time.Duration) look { return pick(at >= crawl, letters, picture) }), nil
	}
	if err := Markers(st, library.Parts{Places: st}, nil, fake)(ctx, jobs[0].Subject); err != nil || read != 1 {
		t.Fatalf("read %d, %v", read, err)
	}
	page, err := st.Title(ctx, uuid.UUID{}, jobs[0].Subject)
	if err != nil {
		t.Fatal(err)
	}
	got := page.Versions[0].Markers
	if len(got) != 1 || got[0].Kind != domain.MarkerCredits || got[0].Source != domain.MarkerByBlackFrames ||
		got[0].StartMS != crawl.Milliseconds() || got[0].EndMS != (2*time.Hour).Milliseconds() {
		t.Errorf("markers %+v, want the credits from 1:52:00 found by black frames", got)
	}
	if err := Markers(st, library.Parts{Places: st}, nil, fake)(ctx, jobs[0].Subject); err != nil || read != 1 {
		t.Errorf("asked again: read %d, %v; want the film passed over", read, err)
	}
	if n, err := st.QueueFilmMarkers(ctx, domain.JobDueWindow); err != nil || n != 0 {
		t.Errorf("the backfill queued %d (%v), want the film read already", n, err)
	}
}
