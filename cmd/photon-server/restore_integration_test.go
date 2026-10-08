//go:build integration

package main

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/kv"
	"github.com/olivertgwalton/photon-server/internal/store"
	"github.com/olivertgwalton/photon-server/internal/store/storetest"
)

// migratedServer answers a migrated database of its own, its server's id, and that server's keys.
func migratedServer(t *testing.T) (string, uuid.UUID, *kv.KV) {
	t.Helper()
	db := storetest.FreshDatabase(t)
	log := slog.New(slog.DiscardHandler)
	if err := store.Migrate(t.Context(), db, log); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(t.Context(), db, log)
	if err != nil {
		t.Fatal(err)
	}
	id, err := st.ServerID(t.Context())
	st.Close()
	if err != nil {
		t.Fatal(err)
	}
	cache, err := kv.Open(os.Getenv("TEST_VALKEY_URL"), id)
	if err != nil {
		t.Fatalf("TEST_VALKEY_URL: %v", err)
	}
	t.Cleanup(func() {
		if _, err := cache.Clear(context.Background()); err != nil {
			t.Error(err)
		}
		cache.Close()
	})
	return db, id, cache
}

func TestANodeWaitsWhileARestoreIsUnderwayAndStartsOnceItEnds(t *testing.T) {
	db, _, cache := migratedServer(t)
	valkeyURL := os.Getenv("TEST_VALKEY_URL")
	if underway, err := restoreUnderway(t.Context(), db, valkeyURL); err != nil || underway {
		t.Fatalf("with no restore: %v %v, want the node to start", underway, err)
	}
	r := domain.Restore{Dump: "photon-20261008T120000Z.dump", Node: uuid.NewV7(), Started: time.Now().UTC(), Phase: domain.RestoreStopping}
	if _, err := cache.BeginRestore(t.Context(), r, time.Minute); err != nil {
		t.Fatal(err)
	}
	if underway, err := restoreUnderway(t.Context(), db, valkeyURL); err != nil || !underway {
		t.Errorf("with a restore under way: %v %v, want the node to wait", underway, err)
	}
	// As a restore no node keeps any longer lapses.
	if err := cache.EndRestore(t.Context()); err != nil {
		t.Fatal(err)
	}
	if underway, err := restoreUnderway(t.Context(), db, valkeyURL); err != nil || underway {
		t.Errorf("once it has gone: %v %v, want the node to start", underway, err)
	}
}
