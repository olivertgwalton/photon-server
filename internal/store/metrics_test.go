//go:build integration

package store

import (
	"testing"
	"time"
	"uuid"

	"github.com/google/go-cmp/cmp"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
)

// The job queue's load is counted by kind and state, with how long the queued job waiting longest
// has been due now; one not due yet, or held for the maintenance window, has not waited.
func TestJobLoadsSayHowLongTheQueueHasWaited(t *testing.T) {
	s := migrated(t)
	enqueueN(t, s, 4)
	if _, err := s.ClaimJobs(t.Context(), []domain.JobKind{domain.JobKeyframes}, nil, uuid.NewV7(), time.Minute, 1); err != nil {
		t.Fatal(err)
	}
	// Of the three queued: one put off for a day, one held for the maintenance window three hours,
	// and one due now an hour.
	if _, err := s.pool.Exec(t.Context(), `
		UPDATE jobs SET
			run_after = CASE
				WHEN id = (SELECT min(id) FROM jobs WHERE state = 'queued') THEN now() + interval '1 day'
				WHEN id = (SELECT max(id) FROM jobs WHERE state = 'queued') THEN now() - interval '3 hours'
				ELSE now() - interval '1 hour' END,
			due = CASE WHEN id = (SELECT max(id) FROM jobs WHERE state = 'queued') THEN 'window' ELSE 'now' END
		WHERE state = 'queued'`); err != nil {
		t.Fatal(err)
	}
	loads, err := s.JobLoads(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	byState := map[domain.JobState]JobLoad{}
	for _, l := range loads {
		byState[l.State] = l
	}
	queued, running := byState[domain.JobQueued], byState[domain.JobRunning]
	if queued.Count != 3 || running.Count != 1 || !running.Due.IsZero() {
		t.Errorf("loads = %+v, want 3 queued and 1 running", loads)
	}
	if waited := time.Since(queued.Due); waited < 59*time.Minute || waited > 2*time.Hour {
		t.Errorf("the queue has waited %v, want about an hour", waited)
	}
}

func TestItemCountsCountTitlesByKind(t *testing.T) {
	s := migrated(t)
	films, err := s.AddLibrary(t.Context(), "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	for _, title := range []string{"Alien", "Heat"} {
		addItem(t, s, model.Item{LibraryID: films.ID, Kind: domain.ItemMovie, Title: title, SortTitle: title, Folder: title})
	}
	got, err := s.ItemCounts(t.Context(), []domain.ItemKind{domain.ItemMovie, domain.ItemEpisode})
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(map[domain.ItemKind]int{domain.ItemMovie: 2}, got); diff != "" {
		t.Errorf("counts (-want +got):\n%s", diff)
	}
}

// A library's storage is the bytes on its disk: an hd and an ultra hd copy of one film are two
// files, while one file read under two paths, or under two episode numbers, is one.
func TestItemBytesCountSharedBytesOnce(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	films, err := s.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	shows, err := s.AddLibrary(ctx, "Shows", domain.LibraryShows, "/srv/shows")
	if err != nil {
		t.Fatal(err)
	}
	copyOf := func(key, rel string, size int64) Copy {
		return Copy{ContentKey: []byte(key), Parts: []Part{{RelPath: rel, Size: size, ModTime: time.Unix(0, 0), Facts: &domain.Facts{Container: "mkv"}}}}
	}
	const hd, ultraHD, episode = 8_000_000_000, 60_000_000_000, 2_000_000_000
	heat := Film{Title: "Heat", Folder: "Heat", Copies: []Copy{copyOf("heat-hd", "Heat/Heat.1080p.mkv", hd), copyOf("heat-uhd", "Heat/Heat.2160p.mkv", ultraHD)}}
	if _, err := s.SaveFolder(ctx, films.ID, "Heat", []byte("v1"), []Film{heat}, nil); err != nil {
		t.Fatal(err)
	}
	again := Film{Title: "Heat", Folder: "Heat (1995)", Copies: []Copy{copyOf("heat-hd", "Heat (1995)/Heat.mkv", hd)}}
	if _, err := s.SaveFolder(ctx, films.ID, "Heat (1995)", []byte("v1"), []Film{again}, nil); err != nil {
		t.Fatal(err)
	}
	eps := []Episode{
		{Season: 1, Episodes: []int{1}, Title: "S1E1", Folder: "Wire", ByNumber: true, Copies: []Copy{copyOf("e1", "Wire/S01E01.mkv", episode)}},
		{Season: 1, Episodes: []int{2}, Title: "S1E2", Folder: "Wire", ByNumber: true, Copies: []Copy{copyOf("e1", "Wire/S01E02.mkv", episode)}},
	}
	if _, err := s.SaveShowFolder(ctx, shows.ID, "Wire", []byte("v1"), Show{Title: "The Wire", Folder: "Wire"}, eps, nil); err != nil {
		t.Fatal(err)
	}
	var paths, episodeVersions int
	if err := s.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM part_files), (SELECT count(*) FROM versions v JOIN items i ON i.id = v.item_id WHERE i.kind = 'episode')`).
		Scan(&paths, &episodeVersions); err != nil || paths != 5 || episodeVersions != 2 {
		t.Fatalf("%d paths and %d episode versions, %v; want 5 and 2", paths, episodeVersions, err)
	}
	got, err := s.ItemBytes(ctx, []domain.ItemKind{domain.ItemMovie, domain.ItemEpisode})
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(map[domain.ItemKind]int64{domain.ItemMovie: hd + ultraHD, domain.ItemEpisode: episode}, got); diff != "" {
		t.Errorf("bytes (-want +got):\n%s", diff)
	}
}
