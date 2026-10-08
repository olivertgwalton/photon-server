//go:build integration

package main

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/backup"
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
		_, _ = cache.Clear(context.Background())
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

func TestTheNodeRestoringRecordsHowTheRestoreEnded(t *testing.T) {
	tools := map[string]string{}
	for _, name := range []string{"pg_dump", "pg_restore", "psql"} {
		path, err := exec.LookPath(name)
		if err != nil {
			t.Skipf("%s is not on the PATH", name)
		}
		tools[name] = path
	}
	db, id, cache := migratedServer(t)
	log := slog.New(slog.DiscardHandler)
	file, err := backup.Dumper{PGDump: tools["pg_dump"], URL: db, Dir: t.TempDir()}.Dump(t.Context(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	r := backup.Restorer{PGRestore: tools["pg_restore"], PSQL: tools["psql"], DatabaseURL: db, ValkeyURL: os.Getenv("TEST_VALKEY_URL"), Log: log}
	restore := domain.Restore{Dump: filepath.Base(file), Node: uuid.NewV7(), Started: time.Now().UTC(), Phase: domain.RestoreStopping}

	broken := filepath.Join(t.TempDir(), "photon-20261008T120000Z.dump")
	if err := os.WriteFile(broken, []byte("not a dump"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.BeginRestore(t.Context(), restore, time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := lead(t.Context(), log, r, broken, id, restore); err == nil {
		t.Fatal("restoring a file that is no dump succeeded")
	}
	if o, ok, err := cache.LastRestore(t.Context()); err != nil || !ok || o.Result != domain.RestoreFailed || o.Reason == "" {
		t.Errorf("after a restore that failed: %+v %v %v, want it failed, saying why", o, ok, err)
	}
	if _, ok, _ := cache.Restoring(t.Context()); ok {
		t.Error("a restore that failed is still under way, holding every node back")
	}

	if _, err := cache.BeginRestore(t.Context(), restore, time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := lead(t.Context(), log, r, file, id, restore); err != nil {
		t.Fatal(err)
	}
	if o, ok, err := cache.LastRestore(t.Context()); err != nil || !ok || o.Result != domain.RestoreSucceeded || o.Dump != restore.Dump {
		t.Errorf("after restoring: %+v %v %v, want it succeeded", o, ok, err)
	}
	if _, ok, _ := cache.Restoring(t.Context()); ok {
		t.Error("a restore done is still under way, holding every node back")
	}
}
