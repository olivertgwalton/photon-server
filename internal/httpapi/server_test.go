package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/hls"
	"github.com/olivertgwalton/photon-server/internal/kv"
	"github.com/olivertgwalton/photon-server/internal/media"
)

// fakeBackend answers its version, or fails as one that cannot be reached.
type fakeBackend struct {
	version string
	nodes   []kv.Node
}

func (f fakeBackend) Version(context.Context) (string, error) {
	if f.version == "" {
		return "", errors.New("dial tcp 10.0.0.9:6379: connection refused")
	}
	return f.version, nil
}

func (f fakeBackend) Nodes(context.Context) ([]kv.Node, error) { return f.nodes, nil }

func TestAnAdminSeesHowTheServerIsSetUp(t *testing.T) {
	node, peer := uuid.NewV7(), uuid.NewV7()
	seen := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	svc := Services{
		Auth: fakeAuth{}, HLS: fakeHLS{}, TrustedProxies: []netip.Prefix{netip.MustParsePrefix("172.16.0.0/12")},
		Setup: Setup{
			Started: seen.Add(-time.Hour), Node: node, Listen: ":8640",
			Tools:     media.Tools{FFmpeg: media.Tool{Path: "/usr/bin/ffmpeg", Version: "8.0"}, Chromaprint: true},
			Encoder:   hls.Hardware{Accel: domain.AccelVAAPI, Device: "/dev/dri/renderD128"},
			Discovery: domain.DiscoveryOff, MetadataLanguage: "en-GB", CacheDir: t.TempDir(), BackupDir: t.TempDir() + "/missing",
		},
		Postgres: fakeBackend{version: "18.1"},
		Valkey:   fakeBackend{version: "9.0.0", nodes: []kv.Node{{ID: peer, Address: "http://10.0.0.5:8640", Seen: seen}}},
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
	if got.Name != "den" || got.NodeID != node || got.Encoder.Acceleration != domain.AccelVAAPI || got.TranscodeLimit != 4 ||
		got.Discovery != domain.DiscoveryOff || !got.Chromaprint || got.MetadataLanguage != "en-GB" {
		t.Errorf("setup = %+v", got)
	}
	if len(got.TrustedProxies) != 1 || got.TrustedProxies[0] != "172.16.0.0/12" {
		t.Errorf("trusted proxies = %v", got.TrustedProxies)
	}
	if got.Folders.Cache.FreeBytes == nil || *got.Folders.Cache.FreeBytes <= 0 || got.Folders.Backups.FreeBytes != nil {
		t.Errorf("folders = %+v, want the cache's free space and none for a folder not there", got.Folders)
	}
	if got.Postgres != (backendJSON{true, "18.1"}) || got.Valkey != (backendJSON{true, "9.0.0"}) {
		t.Errorf("backends = %+v, %+v", got.Postgres, got.Valkey)
	}
	if len(got.Nodes) != 1 || got.Nodes[0] != (nodeJSON{peer, "http://10.0.0.5:8640", seen}) {
		t.Errorf("nodes = %+v", got.Nodes)
	}

	svc.Valkey = fakeBackend{}
	got = serverJSON{}
	if rec := get(svc, goodToken); rec.Code != http.StatusOK || json.NewDecoder(rec.Body).Decode(&got) != nil {
		t.Fatalf("with Valkey down: %d %s", rec.Code, rec.Body)
	}
	if got.Valkey.Reachable || len(got.Nodes) != 0 || !got.Postgres.Reachable {
		t.Errorf("with Valkey down: %+v, nodes %v", got.Valkey, got.Nodes)
	}
}
