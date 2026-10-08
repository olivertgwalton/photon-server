package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// A device being paired is told the web app's page to enter its code at, and the same with the
// code filled in: at the public URL, else where the device reached the server.
func TestAPairingSaysWhereToEnterItsCode(t *testing.T) {
	proxy := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
	for _, tc := range []struct {
		name, public string
		peer, from   string
		want         string
	}{
		{"set", "https://photon.example/app", "192.0.2.1:5000", "", "https://photon.example/app/link"},
		{"unset", "", "192.0.2.1:5000", "", "http://den.local:8640/link"},
		{"behind a trusted proxy on TLS", "", "10.0.0.2:5000", "https", "https://den.local:8640/link"},
		{"told TLS by anyone else", "", "192.0.2.1:5000", "https", "http://den.local:8640/link"},
	} {
		api := New(slog.New(slog.DiscardHandler), domain.Info{}, Services{
			Auth: fakeAuth{}, Limits: &fakeLimiter{}, Reach: reaching(t, domain.Network{TrustedProxies: proxy, PublicURL: tc.public}),
		})
		req := httptest.NewRequest(http.MethodPost, "http://den.local:8640/api/v1/auth/pairings", strings.NewReader(`{"device": "TV", "client": "Photon"}`))
		req.RemoteAddr = tc.peer
		if tc.from != "" {
			req.Header.Set("X-Forwarded-Proto", tc.from)
		}
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		var got pairingStartJSON
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || rec.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", tc.name, rec.Code, rec.Body)
		}
		if got.VerificationURI != tc.want || got.VerificationURIComplete != tc.want+"?code=BCDF-GHJK" {
			t.Errorf("%s: %q and %q, want %q with the code", tc.name, got.VerificationURI, got.VerificationURIComplete, tc.want)
		}
	}
	for _, bad := range []string{"photon.example", "ftp://photon.example", "https://photon.example/?x=1", "https://u:p@photon.example"} {
		if _, err := parsePublicURL(bad); err == nil {
			t.Errorf("public URL %q taken", bad)
		}
	}
}
