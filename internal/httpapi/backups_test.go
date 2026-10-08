package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/backup"
	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/nodecall"
	"github.com/olivertgwalton/photon-server/internal/playback"
)

func TestAnAdminListsAndDownloadsThisNodesDumps(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"photon-20261005T120000Z.dump": "older",
		"photon-20261008T120000Z.dump": "newer",
		"notes.txt":                    "not a dump",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	secret := filepath.Join(filepath.Dir(dir), "secret.dump")
	if err := os.WriteFile(secret, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	node := uuid.NewV7()
	api := New(slog.New(slog.DiscardHandler), domain.Info{}, Services{
		Auth: fakeAuth{}, Setup: Setup{BackupDir: dir}, Backups: fakeRestores{},
		Placer: playback.NewPlacer(fakeBackend{}, func() domain.Node { return domain.Node{ID: node, Name: "nas"} }, nil, nodecall.Key{}),
	})
	get := func(path, token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		return rec
	}

	if rec := get("/api/v1/admin/backups", memberToken); rec.Code != http.StatusForbidden {
		t.Errorf("a member listing: %d, want 403", rec.Code)
	}
	var list backupsJSON
	if rec := get("/api/v1/admin/backups", goodToken); rec.Code != http.StatusOK || json.NewDecoder(rec.Body).Decode(&list) != nil {
		t.Fatalf("listing: %d %s", rec.Code, rec.Body)
	}
	if list.NodeID != node || list.NodeName != "nas" || len(list.Items) != 2 ||
		list.Items[0].Name != "photon-20261008T120000Z.dump" || list.Items[0].SizeBytes != 5 {
		t.Errorf("listed %+v, want the two dumps, the newest first, on nas", list)
	}

	rec := get("/api/v1/admin/backups/photon-20261008T120000Z.dump", goodToken)
	if rec.Code != http.StatusOK || rec.Body.String() != "newer" ||
		rec.Header().Get("Content-Disposition") != `attachment; filename="photon-20261008T120000Z.dump"` {
		t.Errorf("download: %d %q %v", rec.Code, rec.Body, rec.Header())
	}
	if rec := get("/api/v1/admin/backups/photon-20261008T120000Z.dump", memberToken); rec.Code != http.StatusForbidden {
		t.Errorf("a member downloading: %d, want 403", rec.Code)
	}
	for _, path := range []string{
		"/api/v1/admin/backups/notes.txt",
		"/api/v1/admin/backups/..%2Fsecret.dump",
		"/api/v1/admin/backups/photon-20261001T120000Z.dump",
	} {
		if rec := get(path, goodToken); rec.Code != http.StatusNotFound {
			t.Errorf("%s: %d %q, want 404", path, rec.Code, rec.Body)
		}
	}
}

// fakeRestores begins as the server's do, so refusals are its own, with no Valkey to ask the
// other nodes with, and answers a restore under way and the last one's outcome as given.
type fakeRestores struct {
	backup.Restores
	underway *domain.Restore
	last     *domain.RestoreOutcome
}

func (f fakeRestores) Underway(context.Context) (domain.Restore, bool, error) {
	if f.underway == nil {
		return domain.Restore{}, false, nil
	}
	return *f.underway, true, nil
}

func (f fakeRestores) Last(context.Context) (domain.RestoreOutcome, bool, error) {
	if f.last == nil {
		return domain.RestoreOutcome{}, false, nil
	}
	return *f.last, true, nil
}

func TestARestoreIsRefusedForADumpThisNodeLacksOrANewerOne(t *testing.T) {
	dir := t.TempDir()
	name := "photon-20261008T120000Z.dump"
	if err := os.WriteFile(filepath.Join(dir, name), []byte("dump"), 0o600); err != nil {
		t.Fatal(err)
	}
	// pg_restore, telling of a dump made at a schema version no binary has yet.
	pgRestore := filepath.Join(t.TempDir(), "pg_restore")
	script := "#!/bin/sh\nprintf 'COPY public.goose_db_version (id, version_id, is_applied, tstamp) FROM stdin;\\n1\\t99999\\tt\\tnow\\n\\\\.\\n'\n"
	if err := os.WriteFile(pgRestore, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	node := uuid.NewV7()
	started := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	restores := fakeRestores{
		Restorer: backup.Restorer{PGRestore: pgRestore}, Dir: dir,
		underway: &domain.Restore{Dump: name, Node: node, Started: started, Phase: domain.RestoreRestoring},
		last:     &domain.RestoreOutcome{Dump: name, At: started, Result: domain.RestoreFailed, Reason: "nodes still connected"},
	}
	api := New(slog.New(slog.DiscardHandler), domain.Info{}, Services{
		Auth: fakeAuth{}, Setup: Setup{BackupDir: dir}, Backups: restores,
		Placer: playback.NewPlacer(fakeBackend{}, func() domain.Node { return domain.Node{ID: node, Name: "nas"} }, nil, nodecall.Key{}),
	})
	do := func(method, path, token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		return rec
	}
	if rec := do(http.MethodPost, "/api/v1/admin/backups/"+name+"/restore", memberToken); rec.Code != http.StatusForbidden {
		t.Errorf("a member restoring: %d, want 403", rec.Code)
	}
	if rec := do(http.MethodPost, "/api/v1/admin/backups/photon-20261001T120000Z.dump/restore", goodToken); rec.Code != http.StatusNotFound {
		t.Errorf("a dump this node does not keep: %d %s, want 404", rec.Code, rec.Body)
	}
	rec := do(http.MethodPost, "/api/v1/admin/backups/"+name+"/restore", goodToken)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "newer than this photon-server") {
		t.Errorf("a newer dump: %d %s, want 409 saying why", rec.Code, rec.Body)
	}
	var list backupsJSON
	if rec := do(http.MethodGet, "/api/v1/admin/backups", goodToken); rec.Code != http.StatusOK || json.NewDecoder(rec.Body).Decode(&list) != nil {
		t.Fatalf("listing: %d %s", rec.Code, rec.Body)
	}
	if list.Restoring == nil || list.Restoring.Phase != domain.RestoreRestoring || list.Restoring.Dump != name {
		t.Errorf("restoring = %+v, want the restore under way", list.Restoring)
	}
	if list.LastRestore == nil || list.LastRestore.Result != domain.RestoreFailed || list.LastRestore.Reason != "nodes still connected" {
		t.Errorf("last restore = %+v, want how it ended", list.LastRestore)
	}
}
