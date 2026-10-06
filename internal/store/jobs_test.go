//go:build integration

package store

import (
	"errors"
	"slices"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
	"github.com/olivertgwalton/photon-server/internal/store/query"
)

func enqueueN(t *testing.T, s *Store, n int) {
	t.Helper()
	err := s.q.Transaction(func(tx *query.Query) error {
		for range n {
			if err := enqueue(t.Context(), tx, domain.JobKeyframes, model.UUID(uuid.NewV7())); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentClaimsNeverShareAJob(t *testing.T) {
	s := migrated(t)
	enqueueN(t, s, 60)
	var mu sync.Mutex
	seen := map[int64]int{}
	var wg sync.WaitGroup
	for range 6 {
		wg.Go(func() {
			node := uuid.NewV7()
			for {
				jobs, err := s.ClaimJobs(t.Context(), []domain.JobKind{domain.JobKeyframes}, node, time.Minute, 4)
				if err != nil {
					t.Error(err)
					return
				}
				if len(jobs) == 0 {
					return
				}
				mu.Lock()
				for _, j := range jobs {
					seen[j.ID]++
				}
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if len(seen) != 60 {
		t.Errorf("%d jobs claimed, want 60", len(seen))
	}
	for id, n := range seen {
		if n != 1 {
			t.Errorf("job %d claimed %d times", id, n)
		}
	}
}

func TestEnqueueIsIdempotent(t *testing.T) {
	s := migrated(t)
	subject := model.UUID(uuid.NewV7())
	for range 2 {
		err := s.q.Transaction(func(tx *query.Query) error { return enqueue(t.Context(), tx, domain.JobKeyframes, subject) })
		if err != nil {
			t.Fatal(err)
		}
	}
	if n, err := s.q.Job.WithContext(t.Context()).Count(); err != nil || n != 1 {
		t.Errorf("%d jobs for one subject (err %v), want 1", n, err)
	}
}

func TestFailedJobsBackOffThenDie(t *testing.T) {
	s := migrated(t)
	enqueueN(t, s, 1)
	kinds := []domain.JobKind{domain.JobKeyframes}
	node := uuid.NewV7()
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		jobs, err := s.ClaimJobs(t.Context(), kinds, node, time.Minute, 1)
		if err != nil || len(jobs) != 1 {
			t.Fatalf("attempt %d: claimed %d (err %v)", attempt, len(jobs), err)
		}
		if dead, err := s.FailJob(t.Context(), jobs[0], errors.New("unreadable")); err != nil || dead != (attempt == maxAttempts) {
			t.Fatalf("attempt %d: dead %v, %v; want dead on the last", attempt, dead, err)
		}
		if again, _ := s.ClaimJobs(t.Context(), kinds, node, time.Minute, 1); len(again) != 0 {
			t.Fatalf("attempt %d: a failed job was claimable before its backoff", attempt)
		}
		j := s.q.Job
		if _, err := j.WithContext(t.Context()).Where(j.ID.Eq(jobs[0].ID)).UpdateSimple(j.RunAfter.Value(time.Now().Add(-time.Second))); err != nil {
			t.Fatal(err)
		}
	}
	job, err := s.q.Job.WithContext(t.Context()).Take()
	if err != nil {
		t.Fatal(err)
	}
	if job.State != domain.JobDead || job.LastError == nil || *job.LastError != "unreadable" {
		t.Errorf("after %d failures: state %q, error %v; want dead with the reason", maxAttempts, job.State, job.LastError)
	}
	counts, dead, err := s.JobQueue(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(counts) != 1 || counts[0] != (JobCount{Kind: domain.JobKeyframes, State: domain.JobDead, Count: 1}) ||
		len(dead) != 1 || dead[0].ID != job.ID || dead[0].Error != "unreadable" {
		t.Errorf("queue = %+v, %+v; want the one dead job with its reason", counts, dead)
	}
	if err := s.RetryJob(t.Context(), job.ID); err != nil {
		t.Fatal(err)
	}
	if again, _ := s.ClaimJobs(t.Context(), kinds, node, time.Minute, 1); len(again) != 1 || again[0].Attempts != 1 {
		t.Errorf("after a retry: claimed %+v, want the job on a fresh first attempt", again)
	}
	if err := s.RetryJob(t.Context(), job.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("retrying a job that is not dead: %v, want ErrNotFound", err)
	}
}

func TestAPostponedJobNeverDies(t *testing.T) {
	s := migrated(t)
	enqueueN(t, s, 1)
	kinds := []domain.JobKind{domain.JobKeyframes}
	node := uuid.NewV7()
	for attempt := range maxAttempts + 2 {
		jobs, err := s.ClaimJobs(t.Context(), kinds, node, time.Minute, 1)
		if err != nil || len(jobs) != 1 || jobs[0].Attempts != 1 {
			t.Fatalf("claim %d: %+v (err %v), want the job on its first attempt", attempt, jobs, err)
		}
		if err := s.PostponeJob(t.Context(), jobs[0], time.Hour); err != nil {
			t.Fatal(err)
		}
		if again, _ := s.ClaimJobs(t.Context(), kinds, node, time.Minute, 1); len(again) != 0 {
			t.Fatalf("claim %d: a postponed job was claimable before its delay", attempt)
		}
		j := s.q.Job
		if _, err := j.WithContext(t.Context()).Where(j.ID.Eq(jobs[0].ID)).UpdateSimple(j.RunAfter.Value(time.Now().Add(-time.Second))); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSweepRequeuesExpiredLeases(t *testing.T) {
	s := migrated(t)
	enqueueN(t, s, 2)
	kinds := []domain.JobKind{domain.JobKeyframes}
	if _, err := s.ClaimJobs(t.Context(), kinds, uuid.NewV7(), -time.Second, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ClaimJobs(t.Context(), kinds, uuid.NewV7(), time.Hour, 1); err != nil {
		t.Fatal(err)
	}
	n, err := s.SweepJobs(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("swept %d jobs, want only the one whose lease ran out", n)
	}
}

func TestALeaseIsRenewedOnlyByTheNodeHoldingIt(t *testing.T) {
	s := migrated(t)
	enqueueN(t, s, 1)
	kinds := []domain.JobKind{domain.JobKeyframes}
	first, second := uuid.NewV7(), uuid.NewV7()
	jobs, err := s.ClaimJobs(t.Context(), kinds, first, -time.Second, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SweepJobs(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ClaimJobs(t.Context(), kinds, second, time.Hour, 1); err != nil {
		t.Fatal(err)
	}
	if err := s.ExtendLease(t.Context(), jobs[0].ID, first, time.Hour); !errors.Is(err, domain.ErrLeaseLost) {
		t.Errorf("the node that lost the job renewed it: %v", err)
	}
	if err := s.ExtendLease(t.Context(), jobs[0].ID, second, time.Hour); err != nil {
		t.Errorf("the node holding the job did not renew it: %v", err)
	}
}

func TestAJobAskedForWhileRunningRunsAgain(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	subject := model.UUID(uuid.NewV7())
	ask := func() {
		t.Helper()
		if err := s.q.Transaction(func(tx *query.Query) error {
			return enqueue(ctx, tx, domain.JobIdentify, subject)
		}); err != nil {
			t.Fatal(err)
		}
	}
	claim := func() []domain.Job {
		t.Helper()
		jobs, err := s.ClaimJobs(ctx, []domain.JobKind{domain.JobIdentify}, uuid.NewV7(), time.Minute, 5)
		if err != nil {
			t.Fatal(err)
		}
		return jobs
	}

	ask()
	running := claim()
	ask()
	ask()
	if len(claim()) != 0 {
		t.Fatal("a job asked for while running was claimed twice at once")
	}
	if err := s.CompleteJob(ctx, running[0].ID); err != nil {
		t.Fatal(err)
	}
	again := claim()
	if len(again) != 1 || again[0].Attempts != 1 {
		t.Fatalf("after finishing, claimed %+v; want the job once more, on a fresh first attempt", again)
	}
	if err := s.CompleteJob(ctx, again[0].ID); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.q.Job.WithContext(ctx).Count(); n != 0 {
		t.Errorf("%d jobs left after the rerun finished, want none", n)
	}

	ask()
	for job := claim(); len(job) > 0; job = claim() {
		job[0].Attempts = maxAttempts
		if _, err := s.FailJob(ctx, job[0], errors.New("tmdb is down")); err != nil {
			t.Fatal(err)
		}
	}
	ask()
	if revived := claim(); len(revived) != 1 || revived[0].Attempts != 1 {
		t.Errorf("a dead job asked for again: claimed %+v, want it on a first attempt", revived)
	}
}

func TestAScanWaitsForItsLibraryToGoQuiet(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	due := func() int {
		t.Helper()
		jobs, err := s.ClaimJobs(ctx, []domain.JobKind{domain.JobScanLibrary}, uuid.NewV7(), time.Minute, 5)
		if err != nil {
			t.Fatal(err)
		}
		return len(jobs)
	}
	for range 3 {
		if err := s.ScanLibrary(ctx, lib.ID, time.Hour); err != nil {
			t.Fatal(err)
		}
	}
	if n := due(); n != 0 {
		t.Errorf("%d scans due while the library is still changing, want none", n)
	}
	if n, _ := s.q.Job.WithContext(ctx).Count(); n != 1 {
		t.Errorf("%d jobs for three changes, want one", n)
	}
	if err := s.ScanLibrary(ctx, lib.ID, 0); err != nil {
		t.Fatal(err)
	}
	if n := due(); n != 1 {
		t.Errorf("%d scans due once the delay has passed, want 1", n)
	}
}

func TestTitlesDueAFreshMatchAreQueued(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	for _, title := range []string{"Heat", "Ronin"} {
		if _, err := s.SaveFolder(ctx, lib.ID, title, []byte("v1"), []Film{{Title: title, Folder: title}}, nil); err != nil {
			t.Fatal(err)
		}
	}
	// Scanning queued both; they ran.
	j := s.q.Job
	if _, err := j.WithContext(ctx).Where(j.Kind.Eq(string(domain.JobIdentify))).Delete(); err != nil {
		t.Fatal(err)
	}
	i := s.q.Item
	heat, _ := i.WithContext(ctx).Where(i.Title.Eq("Heat")).Take()
	if err := s.Identified(ctx, uuid.UUID(heat.ID)); err != nil {
		t.Fatal(err)
	}
	if n, err := s.RefreshStale(ctx); err != nil || n != 1 {
		t.Errorf("queued %d, %v; want Ronin, never matched, alone", n, err)
	}
	never := 0
	if err := s.SetLibrary(ctx, lib.ID, LibraryChange{RefreshDays: &never}); err != nil {
		t.Fatal(err)
	}
	if _, err := j.WithContext(ctx).Where(j.Kind.Eq(string(domain.JobIdentify))).Delete(); err != nil {
		t.Fatal(err)
	}
	if n, err := s.RefreshStale(ctx); err != nil || n != 0 {
		t.Errorf("a library never refreshed: queued %d, %v", n, err)
	}
	if got, _ := s.Library(ctx, lib.ID); got.RefreshDays != 0 {
		t.Errorf("refresh days = %d, want 0", got.RefreshDays)
	}
}

func TestATitleAScanFindsIsMatchedBeforeTheRefresh(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveFolder(ctx, lib.ID, "Heat", []byte("v1"), []Film{{Title: "Heat", Folder: "Heat"}}, nil); err != nil {
		t.Fatal(err)
	}
	j := s.q.Job
	if _, err := j.WithContext(ctx).Where(j.Kind.Eq(string(domain.JobIdentify))).Delete(); err != nil {
		t.Fatal(err)
	}
	if n, err := s.RefreshStale(ctx); err != nil || n != 1 {
		t.Fatalf("queued %d, %v; want Heat", n, err)
	}
	if _, err := s.SaveFolder(ctx, lib.ID, "Ronin", []byte("v1"), []Film{{Title: "Ronin", Folder: "Ronin"}}, nil); err != nil {
		t.Fatal(err)
	}
	claimed, err := s.ClaimJobs(ctx, []domain.JobKind{domain.JobIdentify}, uuid.NewV4(), time.Minute, 1)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claimed %v, %v", claimed, err)
	}
	i := s.q.Item
	if ronin, _ := i.WithContext(ctx).Where(i.Title.Eq("Ronin")).Take(); claimed[0].Subject != uuid.UUID(ronin.ID) {
		t.Error("the scheduled refresh was matched before the title the scan found")
	}
}

func TestAScanAnswersTheFoldersAskedBeforeItStarted(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	for _, folder := range []string{"Heat", "Alien"} {
		if err := s.ScanFolders(ctx, lib.ID, []string{folder}, 0); err != nil {
			t.Fatal(err)
		}
	}
	asked, read, err := s.ScanRequests(ctx, lib.ID)
	if err != nil || !slices.Equal(asked, []string{"Alien", "Heat"}) {
		t.Fatalf("asked %v, %v; want Alien and Heat", asked, err)
	}
	// Heat changes again while the scan runs, after it read its folder.
	if err := s.ScanFolders(ctx, lib.ID, []string{"Heat"}, 0); err != nil {
		t.Fatal(err)
	}
	if err := s.ScanAnswered(ctx, lib.ID, asked, read); err != nil {
		t.Fatal(err)
	}
	if asked, _, _ := s.ScanRequests(ctx, lib.ID); !slices.Equal(asked, []string{"Heat"}) {
		t.Errorf("left %v asked, want Heat for the next scan", asked)
	}
}
