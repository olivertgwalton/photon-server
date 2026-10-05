//go:build integration

package store

import (
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

func TestLeaseHasOneHolder(t *testing.T) {
	s := migrated(t)
	a, b := uuid.NewV7(), uuid.NewV7()
	hold := func(node uuid.UUID, ttl time.Duration) bool {
		t.Helper()
		held, err := s.HoldLease(t.Context(), "scheduler", node, ttl)
		if err != nil {
			t.Fatal(err)
		}
		return held
	}
	if !hold(a, time.Hour) {
		t.Fatal("the first node did not get a free lease")
	}
	if hold(b, time.Hour) {
		t.Fatal("a second node took a lease that has not expired")
	}
	if !hold(a, time.Hour) {
		t.Fatal("the holder could not renew its lease")
	}
	hold(a, -time.Second) // let it lapse
	if !hold(b, time.Hour) {
		t.Fatal("a second node could not take an expired lease")
	}
	if hold(a, time.Hour) {
		t.Fatal("the old holder took the lease back")
	}
}

func TestATaskAskedForIsRecordedUntilItStarts(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	start := time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := s.TaskStarted(ctx, domain.TaskScanLibraries, start); err != nil {
		t.Fatal(err)
	}
	if err := s.TaskFinished(ctx, domain.TaskScanLibraries, start.Add(time.Minute), domain.TaskFailed, errors.New("disk gone")); err != nil {
		t.Fatal(err)
	}
	if err := s.RequestTask(ctx, domain.TaskScanLibraries); err != nil {
		t.Fatal(err)
	}
	states, err := s.TaskStates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := states[domain.TaskScanLibraries]
	if !got.Started.Equal(start) || got.Result != domain.TaskFailed || got.Error != "disk gone" || !got.Requested.After(got.Started) {
		t.Errorf("state = %+v, want the failed run and a request after it", got)
	}
}
