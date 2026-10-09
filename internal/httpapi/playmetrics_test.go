package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"uuid"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/hls"
	"github.com/olivertgwalton/photon-server/internal/library"
	"github.com/olivertgwalton/photon-server/internal/media"
	"github.com/olivertgwalton/photon-server/internal/playback"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// A play refused because every node is encoding as many videos as it may is counted a refusal,
// and no start; one that plays is counted started.
func TestARefusedPlayIsNoStart(t *testing.T) {
	remuxer, err := hls.NewRemuxer(media.Tools{FFmpeg: media.Tool{Path: "ffmpeg"}}, t.TempDir(), t.TempDir(), hls.Hardware{Accel: domain.AccelSoftware}, 1, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	// Another playback holds the only slot.
	video := domain.VideoPlan{Codec: "hevc", Encode: &domain.VideoEncode{Codec: "h264", Width: 1280, Height: 720, BitrateKbps: 2000}}
	if err := (remuxOpener{remuxer}).Open(t.Context(), uuid.NewV7(), store.PlayCopy{Parts: []store.PlayPart{{DurationMS: 60_000}}}, video, nil, domain.SegmentsFMP4, 0); err != nil {
		t.Fatal(err)
	}
	live := &livePlaybacks{m: map[uuid.UUID]domain.Playback{}}
	sessions := playback.NewSessions(live, live, remuxer, func(context.Context, domain.Event) {}, uuid.NewV7())
	placer := alone(remuxOpener{remuxer}, false)
	api := New(slog.New(slog.DiscardHandler), domain.Info{}, Services{
		Copies: noCopies{}, Discover: noDiscoveries{},
		Network: fakeNetwork{}, Auth: fakeAuth{}, Preferences: &fakePreferences{}, Playing: fakePlaying{},
		Playbacks: sessions, Placer: placer, HLS: remuxer, Signer: playback.NewSigner([]byte("key")), Sent: playback.NewSent(),
	})
	transcode := httptest.NewRequest(http.MethodPost, "/api/v1/titles/"+films.String()+"/play", strings.NewReader(
		`{"profile": {"containers": ["matroska"], "video": [{"codec": "h264"}], "audio": [{"codec": "aac"}], "max_bitrate_kbps": 2000, "parts": "each"}}`))
	transcode.Header.Set("Authorization", "Bearer "+goodToken)
	rec := httptest.NewRecorder()
	api.ServeHTTP(rec, transcode)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("a transcode with no slot: %d %s, want 503", rec.Code, rec.Body)
	}
	rec = httptest.NewRecorder()
	api.ServeHTTP(rec, playRequest(`"mp4"`))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"method":"remux"`) {
		t.Fatalf("a remux: %d %s, want it played", rec.Code, rec.Body)
	}
	refused := `# HELP photon_transcode_refusals_total The playbacks this node refused for want of a node to encode them, by why.
# TYPE photon_transcode_refusals_total counter
photon_transcode_refusals_total{reason="full"} 1
photon_transcode_refusals_total{reason="no_encoder"} 0
`
	if err := testutil.CollectAndCompare(placer, strings.NewReader(refused)); err != nil {
		t.Error(err)
	}
	want := `# HELP photon_playback_starts_total The playbacks started by clients asking this node, by how each plays.
# TYPE photon_playback_starts_total counter
photon_playback_starts_total{method="direct"} 0
photon_playback_starts_total{method="remux"} 1
photon_playback_starts_total{method="transcode"} 0
`
	if err := testutil.CollectAndCompare(sessions, strings.NewReader(want), "photon_playback_starts_total"); err != nil {
		t.Error(err)
	}
}

// A file played as it is, asked for in a range over a connection, counts the bytes of the range.
func TestADirectPlaysRangeIsCountedSent(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "Lawrence"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Lawrence", "Lawrence cd1.mkv"), []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}
	sent := playback.NewSent()
	srv := httptest.NewServer(New(slog.New(slog.DiscardHandler), domain.Info{}, Services{
		Copies: noCopies{}, Discover: noDiscoveries{},
		Network: fakeNetwork{}, Auth: fakeAuth{}, Preferences: &fakePreferences{}, Playing: fakePlaying{root: root}, Parts: library.Parts{Places: fakePlaying{root: root}},
		Playbacks: fakePlaybacks{}, HLS: fakeHLS{}, Placer: alone(fakeHLS{}, false), Signer: playback.NewSigner([]byte("key")), Sent: sent,
	}))
	defer srv.Close()
	rec := httptest.NewRecorder()
	srv.Config.Handler.ServeHTTP(rec, playRequest(`"matroska"`))
	var played struct {
		Parts []struct {
			URL string `json:"url"`
		} `json:"parts"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&played); err != nil || len(played.Parts) == 0 {
		t.Fatalf("play: %s, %v", rec.Body, err)
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+played.Parts[0].URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Range", "bytes=2-5")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusPartialContent || string(body) != "2345" {
		t.Fatalf("the range: %s %q, want 206 \"2345\"", resp.Status, body)
	}
	want := `# HELP photon_sent_bytes_total The bytes of media this node sent its players, by how each was delivered.
# TYPE photon_sent_bytes_total counter
photon_sent_bytes_total{delivery="file"} 4
photon_sent_bytes_total{delivery="segment"} 0
`
	if err := testutil.CollectAndCompare(sent, strings.NewReader(want)); err != nil {
		t.Error(err)
	}
}
