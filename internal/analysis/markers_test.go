//go:build integration

package analysis

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/media"
	"github.com/olivertgwalton/photon-server/internal/store"
	"github.com/olivertgwalton/photon-server/internal/store/storetest"
)

func TestASeasonsSharedIntroIsFound(t *testing.T) {
	log := slog.New(slog.DiscardHandler)
	url := storetest.FreshDatabase(t)
	if err := store.Migrate(t.Context(), url, log); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(t.Context(), url, log)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := t.Context()
	root := t.TempDir()
	lib, err := st.AddLibrary(ctx, "TV", domain.LibraryShows, root)
	if err != nil {
		t.Fatal(err)
	}
	const length = 44 * time.Minute
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
			Copies: []store.Copy{{ContentKey: []byte(rel), Parts: []store.Part{{RelPath: rel, Size: 1, ModTime: time.Unix(0, 0), Facts: &media.Facts{
				Duration: length, Streams: []media.Stream{{Kind: domain.StreamAudio, Codec: "aac"}},
			}}}}},
		})
	}
	if _, err := st.SaveShowFolder(ctx, lib.ID, "Lost/Season 1", []byte("v1"), store.Show{Title: "Lost", Folder: "Lost"}, episodes, nil); err != nil {
		t.Fatal(err)
	}
	jobs, err := st.ClaimJobs(ctx, []domain.JobKind{domain.JobMarkers}, uuid.NewV7(), time.Minute, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 0 {
		t.Fatalf("claimed %v at once, want the season left to settle first", jobs)
	}
	if n, err := st.QueueMarkers(ctx); err != nil || n != 0 {
		t.Errorf("the backfill queued %d (%v), want the season already queued", n, err)
	}

	// The first two episodes open with the theme; the third goes straight in. All three end
	// differently.
	theme := points(1, at(50*time.Second))
	asked := 0
	fake := func(_ context.Context, f *os.File, from, span time.Duration) ([]uint32, error) {
		asked++
		episode := uint64(f.Name()[len(f.Name())-5] - '0')
		rest := points(10*episode+uint64(from/time.Minute), at(span))
		if from == 0 && episode < 3 {
			return join(points(episode, at(30*time.Second)), theme, rest), nil
		}
		return rest, nil
	}
	season := seasonOf(t, st, lib.ID)
	parts, err := st.SeasonParts(ctx, season)
	if err != nil || len(parts) != 3 {
		t.Fatalf("season parts %+v, %v", parts, err)
	}
	if err := Markers(st, fake)(ctx, season); err != nil {
		t.Fatal(err)
	}
	for _, p := range parts {
		page, err := st.Title(ctx, uuid.UUID{}, p.Episode)
		if err != nil {
			t.Fatal(err)
		}
		got := page.Versions[0].Markers
		if strings.HasSuffix(p.RelPath, "3.mkv") {
			if len(got) != 0 {
				t.Errorf("the episode without the theme: %+v, want no markers", got)
			}
			continue
		}
		if len(got) != 1 || got[0].Kind != domain.MarkerIntro || got[0].Source != domain.MarkerByFingerprint ||
			!near(got[0].StartMS, 30*time.Second) || !near(got[0].EndMS, 80*time.Second) {
			t.Errorf("%s: %+v, want the theme from 0:30 to 1:20", p.RelPath, got)
		}
	}

	// Asked again with nothing new, the season is passed over.
	asked = 0
	if err := Markers(st, fake)(ctx, season); err != nil || asked != 0 {
		t.Errorf("again: %d fingerprints taken, %v; want none", asked, err)
	}
	if n, err := st.QueueMarkers(ctx); err != nil || n != 0 {
		t.Errorf("the backfill queued %d (%v), want nothing left to compare", n, err)
	}
}

func seasonOf(t *testing.T, st *store.Store, lib uuid.UUID) uuid.UUID {
	t.Helper()
	cards, _, err := st.Wall(t.Context(), lib, store.WallPage{Sort: domain.SortTitle, Limit: 1})
	if err != nil || len(cards) != 1 {
		t.Fatal(cards, err)
	}
	show, err := st.Title(t.Context(), uuid.UUID{}, cards[0].ID)
	if err != nil || len(show.Seasons) != 1 {
		t.Fatal(show.Seasons, err)
	}
	return show.Seasons[0].ID
}
