package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"uuid"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/hls"
	"github.com/olivertgwalton/photon-server/internal/media"
	"github.com/olivertgwalton/photon-server/internal/playback"
)

// Metrics are answered to a client on the local networks, and to anyone else, or anyone naming a
// local address through a proxy not trusted, as though there were none.
func TestMetricsAreAnsweredOnlyOnLocalNetworks(t *testing.T) {
	api := New(slog.New(slog.DiscardHandler), domain.Info{}, Services{
		Network: fakeNetwork{}, TrustedProxies: []netip.Prefix{netip.MustParsePrefix("10.0.0.2/32")},
		Metrics: prometheus.NewRegistry(),
	})
	for _, tc := range []struct {
		peer, forwarded string
		want            int
	}{
		{"127.0.0.1:5000", "", http.StatusOK},
		{"192.168.1.20:5000", "", http.StatusOK},
		{"203.0.113.9:5000", "", http.StatusNotFound},
		{"203.0.113.9:5000", "127.0.0.1", http.StatusNotFound},
		{"10.0.0.2:5000", "192.168.1.20", http.StatusOK},
		{"10.0.0.2:5000", "203.0.113.9", http.StatusNotFound},
	} {
		r := httptest.NewRequest(http.MethodGet, "/metrics", nil)
		r.RemoteAddr = tc.peer
		if tc.forwarded != "" {
			r.Header.Set("X-Forwarded-For", tc.forwarded)
		}
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, r)
		if rec.Code != tc.want {
			t.Errorf("from %s for %q: status %d, want %d", tc.peer, tc.forwarded, rec.Code, tc.want)
		}
	}
	doc, err := Describe(domain.Info{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(doc), `"/metrics"`) {
		t.Error("the API's description lists /metrics")
	}
}

// An admin is told what this node's metrics say, the starts counted as the registry Prometheus
// scrapes counts them; a member is refused.
func TestAnAdminSeesAPlaybackStartedInTheMetrics(t *testing.T) {
	remuxer, err := hls.NewRemuxer(media.Tools{FFmpeg: media.Tool{Path: "ffmpeg"}}, t.TempDir(), t.TempDir(), hls.Hardware{Accel: domain.AccelSoftware}, 2, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	live := &livePlaybacks{m: map[uuid.UUID]domain.Playback{}}
	sessions := playback.NewSessions(live, live, remuxer, func(context.Context, domain.Event) {}, uuid.NewV7())
	placer, sent := alone(remuxOpener{remuxer}, false), playback.NewSent()
	reg := prometheus.NewRegistry()
	reg.MustRegister(sessions, placer, sent, remuxer)
	api := New(slog.New(slog.DiscardHandler), domain.Info{}, Services{
		Network: fakeNetwork{}, Auth: fakeAuth{}, Preferences: &fakePreferences{}, Playing: fakePlaying{},
		Playbacks: sessions, Placer: placer, HLS: remuxer, Signer: playback.NewSigner([]byte("key")), Sent: sent, Metrics: reg,
	})
	if rec := metricsAs(api, memberToken); rec.Code != http.StatusForbidden {
		t.Errorf("a member: %d, want 403", rec.Code)
	}
	rec := httptest.NewRecorder()
	api.ServeHTTP(rec, playRequest(`"mp4"`))
	if rec.Code != http.StatusOK {
		t.Fatalf("a remux: %d %s", rec.Code, rec.Body)
	}
	got := adminMetrics(t, api)
	if len(got.Nodes) != 1 || got.Nodes[0].ID != placer.Self().ID {
		t.Fatalf("nodes %+v, want this one alone", got.Nodes)
	}
	own := got.Nodes[0].Metrics
	families, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	scraped := map[string]float64{}
	for _, f := range families {
		if f.GetName() == "photon_playback_starts_total" {
			for _, m := range f.GetMetric() {
				scraped[label(m, "method")] = m.GetCounter().GetValue()
			}
		}
	}
	if scraped["remux"] != 1 || own.PlaybackStarts[domain.PlayRemux] != scraped["remux"] || own.PlaybackStarts[domain.PlayDirect] != scraped["direct"] {
		t.Errorf("starts %v, scraped %v; want one remux in both", own.PlaybackStarts, scraped)
	}
	if own.TranscodeSlots != 2 || len(own.SegmentWait.Buckets) == 0 || own.At.IsZero() || got.Cluster != nil {
		t.Errorf("slots %d, segment wait %+v, at %v, cluster %+v; want 2 slots, its buckets, a time and no cluster from a node not leading",
			own.TranscodeSlots, own.SegmentWait, own.At, got.Cluster)
	}
}

func adminMetrics(t *testing.T, api *API) metricsJSON {
	t.Helper()
	rec := metricsAs(api, goodToken)
	var got metricsJSON
	if rec.Code != http.StatusOK || json.NewDecoder(rec.Body).Decode(&got) != nil {
		t.Fatalf("metrics: %d %s", rec.Code, rec.Body)
	}
	return got
}

func metricsAs(api *API, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/metrics", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	api.ServeHTTP(rec, req)
	return rec
}
