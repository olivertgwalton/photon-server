package backup

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// fakePGDump writes its environment's password and its arguments into the file it is given.
func fakePGDump(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pg_dump")
	script := "#!/bin/sh\nfor a; do case $a in --file=*) f=${a#--file=};; esac; done\necho \"$PGPASSWORD $*\" > \"$f\"\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestTheNewestDumpsAreKept(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "backups")
	d := Dumper{PGDump: fakePGDump(t), URL: "postgres://photon:s3cret@db/photon", Dir: dir}
	at := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	var made []string
	for day := range 5 {
		name, err := d.Dump(t.Context(), at.AddDate(0, 0, 3*day))
		if err != nil {
			t.Fatal(err)
		}
		made = append(made, filepath.Base(name))
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var kept []string
	for _, e := range entries {
		kept = append(kept, e.Name())
	}
	if !slices.Equal(kept, made[2:]) {
		t.Errorf("kept %v, want the newest three of %v", kept, made)
	}
	got, err := os.ReadFile(filepath.Join(dir, made[4]))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(got), "s3cret ") || strings.Contains(strings.TrimPrefix(string(got), "s3cret "), "s3cret") {
		t.Errorf("pg_dump saw %q; want the password in its environment and nowhere in its arguments", got)
	}
}
