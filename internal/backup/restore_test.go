package backup

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakePGRestore writes, as pg_restore does of a dump's goose_db_version, one recording version.
func fakePGRestore(t *testing.T, version string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pg_restore")
	script := "#!/bin/sh\nprintf 'SET row_security = off;\\nCOPY public.goose_db_version (id, version_id, is_applied, tstamp) FROM stdin;\\n" +
		"1\\t0\\tt\\t2026-10-08 00:56:38\\n2\\t" + version + "\\tt\\t2026-10-08 00:56:38\\n\\\\.\\n'\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestADumpFromANewerServerIsRefusedBeforeAnythingIsTouched(t *testing.T) {
	r := Restorer{
		PGRestore: fakePGRestore(t, "99999"), PSQL: "/nonexistent/psql",
		DatabaseURL: "postgres://nowhere.invalid/photon", ValkeyURL: "valkey://nowhere.invalid:6379",
		Log: slog.New(slog.DiscardHandler),
	}
	err := r.Restore(t.Context(), "photon-20261008T120000Z.dump", io.Discard)
	if err == nil || !strings.Contains(err.Error(), "schema version 99999, newer than this photon-server's") {
		t.Errorf("restoring a newer dump: %v, want it refused as newer", err)
	}
}
