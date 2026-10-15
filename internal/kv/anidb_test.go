//go:build integration

package kv

import (
	"os"
	"testing"
	"time"
	"uuid"
)

// An answer of AniDB's is read again by every node, and one never kept is not.
func TestAnAniDBAnswerIsKeptForItsTime(t *testing.T) {
	k, err := Open(os.Getenv("TEST_VALKEY_URL"), uuid.NewV7())
	if err != nil {
		t.Fatalf("TEST_VALKEY_URL: %v", err)
	}
	t.Cleanup(k.Close)
	ctx := t.Context()
	if err := k.KeepAniDB(ctx, "anime:69", []byte("<anime id=\"69\"/>"), time.Minute); err != nil {
		t.Fatal(err)
	}
	if got, ok, err := k.AniDB(ctx, "anime:69"); err != nil || !ok || string(got) != `<anime id="69"/>` {
		t.Fatalf("kept: %q, %v, %v", got, ok, err)
	}
	if _, ok, err := k.AniDB(ctx, "anime:21"); err != nil || ok {
		t.Errorf("an anime never asked about: %v, %v", ok, err)
	}
}
