package httpapi

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/olivertgwalton/photon-server/internal/domain"
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
	if strings.Contains(string(doc), "/metrics") {
		t.Error("the API's description lists /metrics")
	}
}
