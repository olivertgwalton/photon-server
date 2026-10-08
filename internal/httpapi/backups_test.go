package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"uuid"

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
		Auth: fakeAuth{}, Setup: Setup{BackupDir: dir},
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
