//go:build integration

package backup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/kv"
)

func TestAskingToRestoreTellsEveryNodeAndAllowsOneAtATime(t *testing.T) {
	dir := t.TempDir()
	name := "photon-20261008T120000Z.dump"
	if err := os.WriteFile(filepath.Join(dir, name), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	// A dump of a database at version 1, which every binary is newer than.
	fake := filepath.Join(t.TempDir(), "pg_restore")
	script := "#!/bin/sh\nprintf 'COPY public.goose_db_version (id, version_id, is_applied, tstamp) FROM stdin;\\n1\\t1\\tt\\tnow\\n\\\\.\\n'\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	k, err := kv.Open(os.Getenv("TEST_VALKEY_URL"), uuid.NewV7())
	if err != nil {
		t.Fatalf("TEST_VALKEY_URL: %v", err)
	}
	defer k.Close()
	t.Cleanup(func() { _, _ = k.Clear(context.Background()) })
	var told []domain.Event
	node := uuid.NewV7()
	s := Restores{
		Restorer: Restorer{PGRestore: fake}, Dir: dir, Node: node, KV: k,
		Raise: func(_ context.Context, e domain.Event) { told = append(told, e) },
	}
	if err := s.Begin(t.Context(), name); err != nil {
		t.Fatal(err)
	}
	r, ok, err := s.Underway(t.Context())
	if err != nil || !ok || r.Dump != name || r.Node != node || r.Phase != domain.RestoreStopping {
		t.Errorf("under way: %+v %v %v, want this dump, by this node, its nodes stopping", r, ok, err)
	}
	if len(told) != 1 || told[0].Kind != domain.EventRestoreStarted {
		t.Errorf("told %+v, want the restore started", told)
	}
	if err := s.Begin(t.Context(), name); !errors.Is(err, ErrRestoring) {
		t.Errorf("a second restore: %v, want it refused while the first is under way", err)
	}
}
