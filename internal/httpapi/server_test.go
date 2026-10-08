package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/hls"
	"github.com/olivertgwalton/photon-server/internal/media"
	"github.com/olivertgwalton/photon-server/internal/nodecall"
	"github.com/olivertgwalton/photon-server/internal/playback"
)

// fakeBackend answers its version, or fails as one that cannot be reached.
type fakeBackend struct {
	version string
	nodes   []domain.Node
}

func (f fakeBackend) Version(context.Context) (string, error) {
	if f.version == "" {
		return "", errors.New("dial tcp 10.0.0.9:6379: connection refused")
	}
	return f.version, nil
}

func (f fakeBackend) Nodes(context.Context) ([]domain.Node, error) { return f.nodes, nil }

func TestAnAdminSeesHowTheServerIsSetUp(t *testing.T) {
	node := uuid.NewV7()
	seen := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	svc := Services{
		Auth: fakeAuth{}, HLS: fakeHLS{},
		Setup: Setup{
			Started: seen.Add(-time.Hour), Node: node, Listen: ":8640",
			Tools:            media.Tools{FFmpeg: media.Tool{Path: "/usr/bin/ffmpeg", Version: "8.0"}, Chromaprint: true},
			Encoder:          hls.Hardware{Accel: domain.AccelVAAPI, Device: "/dev/dri/renderD128"},
			MetadataLanguage: "en-GB", CacheDir: t.TempDir(), BackupDir: t.TempDir() + "/missing",
		},
		Postgres: fakeBackend{version: "18.1"},
		Valkey:   fakeBackend{version: "9.0.0"},
		Placer: playback.NewPlacer(fakeBackend{}, func() domain.Node {
			return domain.Node{ID: node, Role: domain.NodeTranscode, LimitSource: domain.LimitSet}
		}, nil, nodecall.Key{}),
	}
	get := func(svc Services, token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/server", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		New(slog.New(slog.DiscardHandler), domain.Info{Name: "den"}, svc).ServeHTTP(rec, req)
		return rec
	}
	if rec := get(svc, memberToken); rec.Code != http.StatusForbidden {
		t.Errorf("a member: %d, want 403", rec.Code)
	}
	var got serverJSON
	if rec := get(svc, goodToken); rec.Code != http.StatusOK || json.NewDecoder(rec.Body).Decode(&got) != nil {
		t.Fatalf("an admin: %d %s", rec.Code, rec.Body)
	}
	if got.Name != "den" || got.NodeID != node || got.Encoder.Acceleration != domain.AccelVAAPI || got.Transcodes != 1 || got.TranscodeLimit != 4 ||
		got.Role != domain.NodeTranscode || got.LimitSource != domain.LimitSet ||
		!got.Chromaprint || got.MetadataLanguage != "en-GB" {
		t.Errorf("setup = %+v", got)
	}
	if got.Folders.Cache.FreeBytes == nil || *got.Folders.Cache.FreeBytes <= 0 || got.Folders.Backups.FreeBytes != nil {
		t.Errorf("folders = %+v, want the cache's free space and none for a folder not there", got.Folders)
	}
	if got.Postgres != (backendJSON{true, "18.1"}) || got.Valkey != (backendJSON{true, "9.0.0"}) {
		t.Errorf("backends = %+v, %+v", got.Postgres, got.Valkey)
	}
	svc.Valkey = fakeBackend{}
	got = serverJSON{}
	if rec := get(svc, goodToken); rec.Code != http.StatusOK || json.NewDecoder(rec.Body).Decode(&got) != nil {
		t.Fatalf("with Valkey down: %d %s", rec.Code, rec.Body)
	}
	if got.Valkey.Reachable || !got.Postgres.Reachable {
		t.Errorf("with Valkey down: %+v", got.Valkey)
	}
}
