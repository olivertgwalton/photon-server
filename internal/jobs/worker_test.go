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
)

type memoryQueue struct {
	mu        sync.Mutex
	pending   []domain.Job
	completed []int64
	failed    []int64
	postponed []int64
	// leaseUntil is when the job last claimed or renewed loses its lease.
	leaseUntil time.Time
	// taken is whether the job's lease was swept and the job claimed by another node.
	taken bool
}

func (q *memoryQueue) ClaimJobs(_ context.Context, _ []domain.JobKind, _ uuid.UUID, lease time.Duration, limit int) ([]domain.Job, error) {
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

func (q *memoryQueue) FailJob(_ context.Context, job domain.Job, _ error) (bool, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.failed = append(q.failed, job.ID)
	return false, nil
}

func (q *memoryQueue) PostponeJob(_ context.Context, job domain.Job, _ time.Duration) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.postponed = append(q.postponed, job.ID)
	return nil
}

func (q *memoryQueue) ExtendLease(_ context.Context, _ int64, _ uuid.UUID, lease time.Duration) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.taken {
		return domain.ErrLeaseLost
	}
	q.leaseUntil = time.Now().Add(lease)
	return nil
}

type ignore struct{}

func (ignore) Raise(context.Context, domain.Event) {}

func (ignore) JobEnded(context.Context, domain.JobKind) {}

func TestWorkerRunsJobsWithinItsSlots(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		q := &memoryQueue{}
		for id := range int64(10) {
			q.pending = append(q.pending, domain.Job{ID: id, Kind: domain.JobKeyframes, Subject: uuid.NewV7()})
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
		w := NewWorker(q, slog.New(slog.DiscardHandler), uuid.NewV7(), 3, map[domain.JobKind]Handler{domain.JobKeyframes: handler}, ignore{}, nil)
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
		q := &memoryQueue{pending: []domain.Job{{ID: 7, Kind: domain.JobKeyframes}}}
		failing := func(context.Context, uuid.UUID) error { return errors.New("no video stream") }
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})
		w := NewWorker(q, slog.New(slog.DiscardHandler), uuid.NewV7(), 1, map[domain.JobKind]Handler{domain.JobKeyframes: failing}, ignore{}, nil)
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

func TestAJobWithNoRoomIsPostponedNotFailed(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		q := &memoryQueue{pending: []domain.Job{{ID: 7, Kind: domain.JobConvert}}}
		busy := func(context.Context, uuid.UUID) error { return ErrNotNow }
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})
		w := NewWorker(q, slog.New(slog.DiscardHandler), uuid.NewV7(), 1, map[domain.JobKind]Handler{domain.JobConvert: busy}, ignore{}, nil)
		go func() {
			w.Run(ctx)
			close(done)
		}()
		synctest.Sleep(10 * time.Second)
		cancel()
		<-done
		if len(q.postponed) != 1 || q.postponed[0] != 7 || len(q.failed) != 0 || len(q.completed) != 0 {
			t.Errorf("postponed %v, failed %v, completed %v; want job 7 postponed", q.postponed, q.failed, q.completed)
		}
	})
}

// A job running for longer than a lease, as converting a film does, keeps its lease throughout,
// so the sweep never hands it to a second worker.
func TestALongJobKeepsItsLease(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		q := &memoryQueue{pending: []domain.Job{{ID: 1, Kind: domain.JobKeyframes}}}
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
		w := NewWorker(q, slog.New(slog.DiscardHandler), uuid.NewV7(), 1, map[domain.JobKind]Handler{domain.JobKeyframes: long}, ignore{}, nil)
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

// A deploy stops jobs part way; each goes back in the queue at once, its attempt given back,
// rather than waiting out its lease.
func TestAJobCutShortByShutdownIsQueuedAgainAtOnce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		q := &memoryQueue{pending: []domain.Job{{ID: 7, Kind: domain.JobScanLibrary}}}
		scanning := func(ctx context.Context, _ uuid.UUID) error {
			<-ctx.Done()
			return ctx.Err()
		}
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})
		w := NewWorker(q, slog.New(slog.DiscardHandler), uuid.NewV7(), 1, map[domain.JobKind]Handler{domain.JobScanLibrary: scanning}, ignore{}, nil)
		go func() {
			w.Run(ctx)
			close(done)
		}()
		synctest.Sleep(10 * time.Second)
		cancel()
		<-done
		if len(q.postponed) != 1 || q.postponed[0] != 7 || len(q.failed) != 0 {
			t.Errorf("postponed %v, failed %v; want job 7 queued again", q.postponed, q.failed)
		}
	})
}

// A worker that lost touch for longer than a lease finds its job taken by another node: it stops
// the job and leaves its outcome to the node that has it.
func TestAJobWhoseLeaseWasLostStopsAndRecordsNothing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		q := &memoryQueue{pending: []domain.Job{{ID: 7, Kind: domain.JobScanLibrary}}}
		var stopped bool
		scanning := func(ctx context.Context, _ uuid.UUID) error {
			select {
			case <-ctx.Done():
				stopped = true
				return ctx.Err()
			case <-time.After(time.Hour):
				return nil
			}
		}
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})
		w := NewWorker(q, slog.New(slog.DiscardHandler), uuid.NewV7(), 1, map[domain.JobKind]Handler{domain.JobScanLibrary: scanning}, ignore{}, nil)
		go func() {
			w.Run(ctx)
			close(done)
		}()
		synctest.Sleep(time.Second)
		q.mu.Lock()
		q.taken = true
		q.mu.Unlock()
		synctest.Sleep(lease)
		cancel()
		<-done
		if !stopped || len(q.completed)+len(q.failed)+len(q.postponed) != 0 {
			t.Errorf("stopped %t, completed %v, failed %v, postponed %v; want the job stopped and nothing recorded",
				stopped, q.completed, q.failed, q.postponed)
		}
	})
}

type endings struct {
	ignore
	mu    sync.Mutex
	ended int
}

func (e *endings) JobEnded(context.Context, domain.JobKind) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.ended++
}

func TestOnlyAJobThatEndsIsDoneInItsBacklog(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		q := &memoryQueue{pending: []domain.Job{{ID: 1, Kind: domain.JobKeyframes}, {ID: 2, Kind: domain.JobKeyframes}}}
		handler := func(_ context.Context, subject uuid.UUID) error {
			if subject == (uuid.UUID{}) {
				return nil
			}
			return errors.New("no video stream")
		}
		q.pending[1].Subject = uuid.NewV7()
		told := &endings{}
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})
		w := NewWorker(q, slog.New(slog.DiscardHandler), uuid.NewV7(), 2, map[domain.JobKind]Handler{domain.JobKeyframes: handler}, told, nil)
		go func() {
			w.Run(ctx)
			close(done)
		}()
		synctest.Sleep(10 * time.Second)
		cancel()
		<-done
		if told.ended != 1 {
			t.Errorf("%d jobs done in the backlog; want the one finished, not the one to be tried again", told.ended)
		}
	})
}
