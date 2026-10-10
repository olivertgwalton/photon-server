package backup

import (
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/olivertgwalton/photon-server/internal/testtool"
)

// fakePGRestore writes, as pg_restore does of a dump's goose_db_version, one recording version.
func fakePGRestore(t *testing.T, version string) string {
	t.Helper()
	return testtool.Script(t, t.TempDir(), "pg_restore", "printf 'SET row_security = off;\\nCOPY public.goose_db_version (id, version_id, is_applied, tstamp) FROM stdin;\\n"+
		"1\\t0\\tt\\t2026-10-08 00:56:38\\n2\\t"+version+"\\tt\\t2026-10-08 00:56:38\\n\\\\.\\n'\n")
}

func TestADumpFromANewerServerIsRefusedBeforeAnythingIsTouched(t *testing.T) {
	r := Restorer{
		PGRestore: fakePGRestore(t, "99999"), PSQL: "/nonexistent/psql",
		DatabaseURL: "postgres://nowhere.invalid/photon", ValkeyURL: "valkey://nowhere.invalid:6379",
		Log: slog.New(slog.DiscardHandler),
	}
	err := r.Restore(t.Context(), "photon-20261008T120000Z.dump", io.Discard)
	if !errors.Is(err, ErrNewer) || !strings.Contains(err.Error(), "schema version 99999") {
		t.Errorf("restoring a newer dump: %v, want it refused as newer", err)
	}
}

// Asking from the web for a dump this node does not keep, or a newer one, is refused before the
// other nodes are asked to stop: these Restores have no Valkey to ask them with.
func TestAskingToRestoreAMissingOrNewerDumpChangesNothing(t *testing.T) {
	dir := t.TempDir()
	name := "photon-20261008T120000Z.dump"
	if err := os.WriteFile(filepath.Join(dir, name), []byte("dump"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := Restores{Restorer: Restorer{PGRestore: fakePGRestore(t, "99999")}, Dir: dir}
	if err := s.Begin(t.Context(), "photon-20261001T120000Z.dump"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("a dump this node does not keep: %v, want no such dump", err)
	}
	if err := s.Begin(t.Context(), "../"+name); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("a name reaching outside the folder: %v, want no such dump", err)
	}
	if err := s.Begin(t.Context(), name); !errors.Is(err, ErrNewer) {
		t.Errorf("a newer dump: %v, want it refused as newer", err)
	}
}
