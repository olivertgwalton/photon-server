package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"reflect"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/hls"
	"github.com/olivertgwalton/photon-server/internal/media"
	"github.com/olivertgwalton/photon-server/internal/nodecall"
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

// Every node running is asked for its metrics, by a request only a node can make: what the nodes
// share is said once, by the node holding the lease, and a node that does not answer is listed as
// unreachable beside those that do.
func TestEveryNodeIsAskedForItsMetrics(t *testing.T) {
	key, err := nodecall.NewKey([]byte("cluster signing key"))
	if err != nil {
		t.Fatal(err)
	}
	gauge := func(reg *prometheus.Registry, name string, value float64, labels ...string) {
		var names, values []string
		for i := 0; i < len(labels); i += 2 {
			names, values = append(names, labels[i]), append(values, labels[i+1])
		}
		g := prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: name, Help: name}, names)
		g.WithLabelValues(values...).Set(value)
		reg.MustRegister(g)
	}
	leading := prometheus.NewRegistry()
	gauge(leading, "photon_jobs", 3, "kind", "identify", "state", "queued")
	gauge(leading, "photon_nodes", 2, "state", "active")
	gauge(leading, "photon_library_bytes", 9_500_000_000_000, "kind", "movie")
	gauge(leading, "photon_task_last_finished_timestamp_seconds", 1_791_460_800, "task", "scan_libraries", "result", "succeeded")
	leader := httptest.NewServer(New(slog.New(slog.DiscardHandler), domain.Info{}, Services{Metrics: leading, NodeKey: key}))
	defer leader.Close()
	gone := httptest.NewServer(http.NotFoundHandler())
	gone.Close()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, leader.URL+metricsPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("a client asking a node for its metrics: %s, want 401", resp.Status)
	}

	own := prometheus.NewRegistry()
	gauge(own, "photon_playbacks", 1, "method", "direct")
	self, beta, gamma := uuid.NewV7(), uuid.NewV7(), uuid.NewV7()
	api := New(slog.New(slog.DiscardHandler), domain.Info{}, Services{
		Auth: fakeAuth{}, Metrics: own, NodeKey: key,
		Valkey: fakeBackend{nodes: []domain.Node{
			{ID: gamma, Name: "gamma", Address: gone.URL, Role: domain.NodeAll, Availability: domain.NodeActive},
			{ID: beta, Name: "beta", Address: leader.URL, Role: domain.NodeTranscode, Availability: domain.NodeDraining},
		}},
		Placer: playback.NewPlacer(fakeBackend{}, func() domain.Node {
			return domain.Node{ID: self, Name: "alpha", Role: domain.NodeAll, Availability: domain.NodeActive}
		}, nil, key),
	})
	got := adminMetrics(t, api)
	if len(got.Nodes) != 3 {
		t.Fatalf("nodes %+v, want alpha, beta and gamma", got.Nodes)
	}
	alpha, b, g := got.Nodes[0], got.Nodes[1], got.Nodes[2]
	if alpha.ID != self || alpha.Reach != reachAnswered || alpha.Metrics == nil || alpha.Metrics.Playbacks[domain.PlayDirect] != 1 {
		t.Errorf("this node: %+v, want it answering its one direct play", alpha)
	}
	if b.ID != beta || b.Reach != reachAnswered || b.Metrics == nil || b.Availability != domain.NodeDraining {
		t.Errorf("the leader: %+v, want it answering, draining", b)
	}
	if g.ID != gamma || g.Reach != reachUnreachable || g.Error == "" || g.Metrics != nil {
		t.Errorf("the node gone: %+v, want it unreachable, saying why", g)
	}
	want := clusterMetricsJSON{
		Node: beta, Jobs: []jobCountJSON{{Kind: domain.JobIdentify, State: domain.JobQueued, Count: 3}},
		OldestDue: map[domain.JobKind]float64{}, Nodes: map[domain.NodeAvailability]int{domain.NodeActive: 2}, LibraryItems: map[domain.ItemKind]int{},
		LibraryBytes: map[domain.ItemKind]int64{domain.ItemMovie: 9_500_000_000_000},
		Tasks:        []taskFinishedJSON{{Task: domain.TaskScanLibraries, Result: domain.TaskSucceeded, FinishedAt: time.Unix(1_791_460_800, 0).UTC()}},
	}
	if got.Cluster == nil || !reflect.DeepEqual(*got.Cluster, want) {
		t.Errorf("cluster %+v, want %+v", got.Cluster, want)
	}
}
