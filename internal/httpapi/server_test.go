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

type fakeReleases struct{ newer *domain.Release }

func (f fakeReleases) Newer() (domain.Release, bool) {
	if f.newer == nil {
		return domain.Release{}, false
	}
	return *f.newer, true
}

func TestAnAdminSeesHowTheServerIsSetUp(t *testing.T) {
	node := uuid.NewV7()
	seen := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	svc := Services{
		Auth: fakeAuth{}, HLS: fakeHLS{hw: hls.Hardware{Accel: domain.AccelVAAPI, Device: "/dev/dri/renderD128"}}, Identity: den(),
		Setup: Setup{
			Started: seen.Add(-time.Hour), Node: node, Listen: ":8640",
			Tools:    media.Tools{FFmpeg: media.Tool{Path: "/usr/bin/ffmpeg", Version: "8.0"}, Chromaprint: true},
			CacheDir: t.TempDir(), BackupDir: t.TempDir() + "/missing",
		},
		Releases: fakeReleases{},
		Postgres: fakeBackend{version: "18.1"},
		Valkey:   fakeBackend{version: "9.0.0"},
		Placer: playback.NewPlacer(fakeBackend{}, func() domain.Node {
			return domain.Node{ID: node, Role: domain.NodeTranscode, LimitSource: domain.LimitSet}
		}, nil, nil, nodecall.Key{}),
	}
	get := func(svc Services, token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/server", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		New(slog.New(slog.DiscardHandler), domain.Info{}, svc).ServeHTTP(rec, req)
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
	if got.NewerRelease != nil {
		t.Errorf("newer release = %+v, want none", got.NewerRelease)
	}
	svc.Releases = fakeReleases{&domain.Release{Version: "v0.7.0", URL: "https://github.com/o/p/releases/tag/v0.7.0"}}
	got = serverJSON{}
	if rec := get(svc, goodToken); rec.Code != http.StatusOK || json.NewDecoder(rec.Body).Decode(&got) != nil {
		t.Fatalf("with a newer release: %d %s", rec.Code, rec.Body)
	}
	if got.NewerRelease == nil || *got.NewerRelease != (releaseJSON{"v0.7.0", "https://github.com/o/p/releases/tag/v0.7.0"}) {
		t.Errorf("newer release = %+v, want v0.7.0 and its page", got.NewerRelease)
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

// keptServer keeps what an admin sets of the server.
type keptServer struct{ set domain.ServerSettings }

func (k *keptServer) SetServerSettings(_ context.Context, set domain.ServerSettings) error {
	k.set = set
	return nil
}

// An admin names the server and sets what its metadata is asked in and whether it looks for a newer
// release, and every node is told.
func TestAnAdminNamesTheServerAndItsLanguage(t *testing.T) {
	kept := &keptServer{}
	events := &fakeEvents{}
	api := New(slog.New(slog.DiscardHandler), domain.Info{}, Services{Auth: fakeAuth{}, Events: events, ServerSettings: kept})
	for body, want := range map[string]int{
		`{"name":" Den ","metadata_language":"en-gb","certification_country":"gb","release_check":"off"}`: http.StatusOK,
		`{"name":"Den","metadata_language":"","release_check":"on"}`:                                      http.StatusBadRequest,
		`{"name":"Den","metadata_language":"not a tag!","release_check":"on"}`:                            http.StatusBadRequest,
		`{"name":"Den","metadata_language":"en-GB","certification_country":"EU","release_check":"on"}`:    http.StatusBadRequest,
		`{"name":"Den\u0007","metadata_language":"en-GB","release_check":"on"}`:                           http.StatusBadRequest,
		`{"name":"Den","metadata_language":"en-GB"}`:                                                      http.StatusBadRequest,
		`{"name":"Den","metadata_language":"en-GB","release_check":"weekly"}`:                             http.StatusBadRequest,
	} {
		if rec := ask(api, http.MethodPut, "/api/v1/admin/server", body); rec.Code != want {
			t.Errorf("%s: %d %s, want %d", body, rec.Code, rec.Body, want)
		}
	}
	if want := (domain.ServerSettings{Name: "Den", Locale: domain.Locale{Language: "en-GB", Country: "GB"}, ReleaseCheck: domain.ReleaseCheckOff}); kept.set != want {
		t.Errorf("kept %+v, want %+v", kept.set, want)
	}
	if len(events.raised) != 1 || events.raised[0].Kind != domain.EventServerChanged {
		t.Errorf("raised %+v, want every node told once", events.raised)
	}
}
