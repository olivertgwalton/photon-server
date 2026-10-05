package jobs

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"testing/synctest"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
)

type memoryQueue struct {
	mu        sync.Mutex
	pending   []store.Job
	completed []int64
	failed    []int64
	// leaseUntil is when the job last claimed or renewed loses its lease.
	leaseUntil time.Time
}

func (q *memoryQueue) ClaimJobs(_ context.Context, _ []domain.JobKind, _ uuid.UUID, lease time.Duration, limit int) ([]store.Job, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	n := min(limit, len(q.pending))
	out := q.pending[:n:n]
	q.pending = q.pending[n:]
	q.leaseUntil = time.Now().Add(lease)
	return out, nil
}

func (q *memoryQueue) CompleteJob(_ context.Context, id int64) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.completed = append(q.completed, id)
	return nil
}

func (q *memoryQueue) FailJob(_ context.Context, job store.Job, _ error) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.failed = append(q.failed, job.ID)
	return nil
}

func (q *memoryQueue) ExtendLease(_ context.Context, _ int64, lease time.Duration) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.leaseUntil = time.Now().Add(lease)
	return nil
}

func TestWorkerRunsJobsWithinItsSlots(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		q := &memoryQueue{}
		for id := range int64(10) {
			q.pending = append(q.pending, store.Job{ID: id, Kind: domain.JobKeyframes, Subject: uuid.NewV7()})
		}
		var mu sync.Mutex
		running, most := 0, 0
		handler := func(context.Context, uuid.UUID) error {
			mu.Lock()
			running++
			most = max(most, running)
			mu.Unlock()
			<-time.After(time.Second)
			mu.Lock()
			running--
			mu.Unlock()
			return nil
		}
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})
		w := NewWorker(q, slog.New(slog.DiscardHandler), uuid.NewV7(), 3, map[domain.JobKind]Handler{domain.JobKeyframes: handler})
		go func() {
			w.Run(ctx)
			close(done)
		}()
		synctest.Sleep(time.Minute)
		cancel()
		<-done
		if most > 3 {
			t.Errorf("%d jobs ran at once with 3 slots", most)
		}
		if len(q.completed) != 10 {
			t.Errorf("%d jobs completed, want 10", len(q.completed))
		}
	})
}

func TestWorkerReportsFailures(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		q := &memoryQueue{pending: []store.Job{{ID: 7, Kind: domain.JobKeyframes}}}
		failing := func(context.Context, uuid.UUID) error { return errors.New("no video stream") }
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})
		w := NewWorker(q, slog.New(slog.DiscardHandler), uuid.NewV7(), 1, map[domain.JobKind]Handler{domain.JobKeyframes: failing})
		go func() {
			w.Run(ctx)
			close(done)
		}()
		synctest.Sleep(10 * time.Second)
		cancel()
		<-done
		if len(q.failed) != 1 || q.failed[0] != 7 || len(q.completed) != 0 {
			t.Errorf("failed %v, completed %v; want job 7 failed once", q.failed, q.completed)
		}
	})
}

// A job running for longer than a lease, as converting a film does, keeps its lease throughout,
// so the sweep never hands it to a second worker.
func TestALongJobKeepsItsLease(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		q := &memoryQueue{pending: []store.Job{{ID: 1, Kind: domain.JobKeyframes}}}
		var lost bool
		long := func(context.Context, uuid.UUID) error {
			for range 60 {
				<-time.After(time.Minute)
				q.mu.Lock()
				lost = lost || time.Now().After(q.leaseUntil)
				q.mu.Unlock()
			}
			return nil
		}
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})
		w := NewWorker(q, slog.New(slog.DiscardHandler), uuid.NewV7(), 1, map[domain.JobKind]Handler{domain.JobKeyframes: long})
		go func() {
			w.Run(ctx)
			close(done)
		}()
		synctest.Sleep(2 * time.Hour)
		cancel()
		<-done
		if lost || len(q.completed) != 1 {
			t.Errorf("lease lost %t, completed %v; want an hour's job to keep its lease and finish", lost, q.completed)
		}
	})
}
