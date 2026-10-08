//go:build integration

package kv

import (
	"os"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

func TestClearingForgetsThisServersKeysAndNoOthers(t *testing.T) {
	open := func() *KV {
		k, err := Open(os.Getenv("TEST_VALKEY_URL"), uuid.NewV7())
		if err != nil {
			t.Fatalf("TEST_VALKEY_URL: %v", err)
		}
		t.Cleanup(k.Close)
		return k
	}
	ours, theirs := open(), open()
	ctx := t.Context()
	for _, k := range []*KV{ours, theirs} {
		if err := k.SavePlayback(ctx, domain.Playback{ID: uuid.NewV7()}, time.Minute); err != nil {
			t.Fatal(err)
		}
		if err := k.SetNode(ctx, domain.Node{ID: uuid.NewV7()}, time.Minute); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := ours.Clear(ctx); err != nil || n != 2 {
		t.Fatalf("cleared %d, %v; want the playbacks and nodes hashes", n, err)
	}
	if p, err := ours.Playbacks(ctx); err != nil || len(p) != 0 {
		t.Errorf("our playbacks after clearing: %v %v", p, err)
	}
	if p, err := theirs.Playbacks(ctx); err != nil || len(p) != 1 {
		t.Errorf("another server's playbacks after clearing ours: %v %v", p, err)
	}
	if n, err := theirs.Nodes(ctx); err != nil || len(n) != 1 {
		t.Errorf("another server's nodes after clearing ours: %v %v", n, err)
	}
	if _, err := theirs.Clear(ctx); err != nil {
		t.Fatal(err)
	}
}
