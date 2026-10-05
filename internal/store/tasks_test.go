//go:build integration

package store

import (
	"testing"
	"time"
	"uuid"
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
