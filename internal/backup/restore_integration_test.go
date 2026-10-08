//go:build integration

package backup

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/kv"
	"github.com/olivertgwalton/photon-server/internal/store"
	"github.com/olivertgwalton/photon-server/internal/store/storetest"
)

func tool(t *testing.T, name string) string {
	t.Helper()
	path, err := exec.LookPath(name)
	if err != nil {
		t.Skipf("%s is not on the PATH", name)
	}
	return path
}

func execSQL(t *testing.T, url, sql string) {
	t.Helper()
	conn, err := pgx.Connect(t.Context(), url)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(context.Background())
	if _, err := conn.Exec(t.Context(), sql); err != nil {
		t.Fatal(err)
	}
}

func country(t *testing.T, url string) string {
	t.Helper()
	conn, err := pgx.Connect(t.Context(), url)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(context.Background())
	var c string
	if err := conn.QueryRow(t.Context(), "SELECT coalesce(certificate_country, '') FROM server").Scan(&c); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestARestorePutsTheDatabaseBackAsTheDumpHadIt(t *testing.T) {
	pgDump, pgRestore, psql := tool(t, "pg_dump"), tool(t, "pg_restore"), tool(t, "psql")
	ctx, log := t.Context(), slog.New(slog.DiscardHandler)
	db := storetest.FreshDatabase(t)
	if err := store.Migrate(ctx, db, log); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(ctx, db, log)
	if err != nil {
		t.Fatal(err)
	}
	id, err := st.ServerID(ctx)
	if err == nil {
		err = st.SetCertificateCountry(ctx, "GB")
	}
	st.Close()
	if err != nil {
		t.Fatal(err)
	}
	file, err := Dumper{PGDump: pgDump, URL: db, Dir: t.TempDir()}.Dump(ctx, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	execSQL(t, db, "UPDATE server SET certificate_country = 'US'; CREATE TABLE stray (x int)")

	valkeyURL := os.Getenv("TEST_VALKEY_URL")
	ours, err := kv.Open(valkeyURL, id)
	if err != nil {
		t.Fatalf("TEST_VALKEY_URL: %v", err)
	}
	defer ours.Close()
	theirs, err := kv.Open(valkeyURL, uuid.NewV7())
	if err != nil {
		t.Fatal(err)
	}
	defer theirs.Close()
	t.Cleanup(func() { _, _ = theirs.Clear(context.Background()) })
	for _, k := range []*kv.KV{ours, theirs} {
		if err := k.SavePlayback(ctx, domain.Playback{ID: uuid.NewV7()}, time.Minute); err != nil {
			t.Fatal(err)
		}
	}

	r := Restorer{PGRestore: pgRestore, PSQL: psql, DatabaseURL: db, ValkeyURL: valkeyURL, Log: log}
	node, err := pgx.Connect(ctx, db+"?application_name=photon-server%20nas")
	if err != nil {
		t.Fatal(err)
	}
	err = r.Restore(ctx, file, &strings.Builder{})
	node.Close(context.Background())
	if err == nil || !strings.Contains(err.Error(), "photon-server nas from ") {
		t.Fatalf("restoring with a node running: %v, want it refused, naming the node", err)
	}
	if c := country(t, db); c != "US" {
		t.Fatalf("a refused restore changed the database: country %q", c)
	}

	// A pg_restore that stops partway through.
	broken := filepath.Join(t.TempDir(), "pg_restore")
	script := "#!/bin/sh\ncase \"$1\" in --data-only) exec " + pgRestore + " \"$@\";; esac\n" + pgRestore + " \"$@\" | head -c 50000\nexit 1\n"
	if err := os.WriteFile(broken, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	failing := r
	failing.PGRestore = broken
	if err := failing.Restore(ctx, file, &strings.Builder{}); err == nil {
		t.Fatal("a restore whose pg_restore failed succeeded")
	}
	if c := country(t, db); c != "US" {
		t.Fatalf("a failed restore changed the database: country %q", c)
	}

	var out strings.Builder
	if err := r.Restore(ctx, file, &out); err != nil {
		t.Fatalf("restoring: %v\n%s", err, out.String())
	}
	if c := country(t, db); c != "GB" {
		t.Errorf("country after restoring: %q, want the dump's GB", c)
	}
	conn, err := pgx.Connect(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	var stray *string
	err = conn.QueryRow(ctx, "SELECT to_regclass('stray')::text").Scan(&stray)
	conn.Close(context.Background())
	if err != nil || stray != nil {
		t.Errorf("a table made after the dump: %v %v, want it gone", stray, err)
	}
	if p, err := ours.Playbacks(ctx); err != nil || len(p) != 0 {
		t.Errorf("the server's playbacks after restoring: %v %v, want none", p, err)
	}
	if p, err := theirs.Playbacks(ctx); err != nil || len(p) != 1 {
		t.Errorf("another server's playbacks after restoring: %v %v, want them untouched", p, err)
	}
	if !strings.Contains(out.String(), "Restored "+file) || !strings.Contains(out.String(), "Cleared 1 ") {
		t.Errorf("said %q", out.String())
	}
}
