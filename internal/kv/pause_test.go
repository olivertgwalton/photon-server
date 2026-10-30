//go:build integration

package kv

import (
	"os"
	"testing"
	"time"
	"uuid"
)

func TestAShortPauseDoesNotCutALongOneShort(t *testing.T) {
	k, err := Open(os.Getenv("TEST_VALKEY_URL"), uuid.NewV7())
	if err != nil {
		t.Fatalf("TEST_VALKEY_URL: %v", err)
	}
	defer k.Close()
	name := "test:" + uuid.NewV4().String()
	if wait, err := k.Paused(t.Context(), name); err != nil || wait != 0 {
		t.Fatalf("never paused: %v, %v; want no wait", wait, err)
	}
	for _, d := range []time.Duration{time.Hour, time.Second} {
		if err := k.Pause(t.Context(), name, d); err != nil {
			t.Fatal(err)
		}
	}
	if wait, err := k.Paused(t.Context(), name); err != nil || wait < 59*time.Minute {
		t.Errorf("paused an hour, then a second: %v, %v; want about an hour", wait, err)
	}
	if wait, err := k.Paused(t.Context(), name+":other"); err != nil || wait != 0 {
		t.Errorf("another provider: %v, %v; want no wait", wait, err)
	}
}
