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
