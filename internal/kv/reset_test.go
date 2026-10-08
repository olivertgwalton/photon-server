//go:build integration

package kv

import (
	"os"
	"testing"
	"time"
	"uuid"
)

func TestAResetCodeIsUsedOnce(t *testing.T) {
	k, err := Open(os.Getenv("TEST_VALKEY_URL"), uuid.NewV7())
	if err != nil {
		t.Fatalf("TEST_VALKEY_URL: %v", err)
	}
	t.Cleanup(k.Close)
	ctx := t.Context()
	profile, code := uuid.NewV7(), []byte("code hash")
	if started, err := k.StartReset(ctx, code, profile, time.Minute); err != nil || !started {
		t.Fatalf("starting: %v, %v", started, err)
	}
	if started, err := k.StartReset(ctx, code, uuid.NewV7(), time.Minute); err != nil || started {
		t.Fatalf("a code in use was taken for another profile: %v, %v", started, err)
	}
	if got, ok, err := k.TakeReset(ctx, code); err != nil || !ok || got != profile {
		t.Fatalf("taking: %v, %v, %v; want %v", got, ok, err, profile)
	}
	if _, ok, err := k.TakeReset(ctx, code); err != nil || ok {
		t.Fatalf("a code was used twice: %v, %v", ok, err)
	}
}
