package backup

import (
	"errors"
	"io/fs"
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

func TestTheDumpsMadeAreListedAndOpenedByNameAlone(t *testing.T) {
	dir := t.TempDir()
	d := Dumper{PGDump: fakePGDump(t), URL: "postgres://db/photon", Dir: dir}
	older, newer := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC), time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	for _, at := range []time.Time{older, newer} {
		if _, err := d.Dump(t.Context(), at); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("not a dump"), 0o600); err != nil {
		t.Fatal(err)
	}
	dumps, err := List(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(dumps) != 2 || !dumps[0].MadeAt.Equal(newer) || !dumps[1].MadeAt.Equal(older) || dumps[0].Size == 0 {
		t.Fatalf("listed %+v, want the two dumps, the newest first", dumps)
	}
	f, err := Open(dir, dumps[0].Name)
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	for _, name := range []string{"notes.txt", "../photon-20261008T120000Z.dump", "photon-20261008T120000Z.dump/../../etc/passwd", "photon-2026.dump"} {
		if _, err := Open(dir, name); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("opening %q: %v, want no such dump", name, err)
		}
	}
	if dumps, err := List(filepath.Join(dir, "missing")); err != nil || len(dumps) != 0 {
		t.Errorf("a folder not yet made: %v %v, want no dumps", dumps, err)
	}
}
