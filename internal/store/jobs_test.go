//go:build integration

package store

import (
	"errors"
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
		if err := s.FailJob(t.Context(), jobs[0], errors.New("unreadable")); err != nil {
			t.Fatal(err)
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
	claim := func() []Job {
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
		if err := s.FailJob(ctx, job[0], errors.New("tmdb is down")); err != nil {
			t.Fatal(err)
		}
	}
	ask()
	if revived := claim(); len(revived) != 1 || revived[0].Attempts != 1 {
		t.Errorf("a dead job asked for again: claimed %+v, want it on a first attempt", revived)
	}
}
