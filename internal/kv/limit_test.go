//go:build integration

package kv

import (
	"os"
	"testing"
	"time"
	"uuid"
)

func TestAllowSpendsABurstThenWaits(t *testing.T) {
	k, err := Open(os.Getenv("TEST_VALKEY_URL"), uuid.NewV7())
	if err != nil {
		t.Fatalf("TEST_VALKEY_URL: %v", err)
	}
	defer k.Close()
	key := "test:" + uuid.NewV4().String()
	l := Limit{Every: time.Minute, Burst: 3}
	for i := range 3 {
		if wait, err := k.Allow(t.Context(), key, l); err != nil || wait != 0 {
			t.Fatalf("request %d of the burst: wait %v, err %v", i+1, wait, err)
		}
	}
	wait, err := k.Allow(t.Context(), key, l)
	if err != nil {
		t.Fatal(err)
	}
	if wait < 50*time.Second || wait > time.Minute {
		t.Errorf("past the burst: wait %v, want about a minute", wait)
	}
	other, err := k.Allow(t.Context(), key+":other", l)
	if err != nil {
		t.Fatal(err)
	}
	if other != 0 {
		t.Error("another key was limited")
	}
}
