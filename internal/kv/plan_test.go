//go:build integration

package kv

import (
	"os"
	"testing"
	"time"
	"uuid"
)

// A play session's plan is answered to whichever node asks, as it was kept.
func TestAPlanIsAnsweredForItsSession(t *testing.T) {
	k, err := Open(os.Getenv("TEST_VALKEY_URL"), uuid.NewV7())
	if err != nil {
		t.Fatalf("TEST_VALKEY_URL: %v", err)
	}
	t.Cleanup(k.Close)
	ctx := t.Context()
	session, plan := uuid.NewV7(), []byte(`{"version":"x","video":{"codec":"h264"}}`)
	if err := k.SavePlan(ctx, session, plan, time.Minute); err != nil {
		t.Fatal(err)
	}
	if got, ok, err := k.Plan(ctx, session); err != nil || !ok || string(got) != string(plan) {
		t.Fatalf("the plan: %q, %v, %v; want %q", got, ok, err, plan)
	}
	if _, ok, err := k.Plan(ctx, uuid.NewV7()); err != nil || ok {
		t.Errorf("a session never planned: %v, %v", ok, err)
	}
}
