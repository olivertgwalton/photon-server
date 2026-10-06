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
// code filled in: at PHOTON_PUBLIC_URL, else where the device reached the server.
func TestAPairingSaysWhereToEnterItsCode(t *testing.T) {
	public, err := ParsePublicURL("https://photon.example/app")
	if err != nil {
		t.Fatal(err)
	}
	proxy := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
	for _, tc := range []struct {
		name       string
		setup      Setup
		peer, from string
		want       string
	}{
		{"set", Setup{PublicURL: public}, "192.0.2.1:5000", "", "https://photon.example/app/link"},
		{"unset", Setup{}, "192.0.2.1:5000", "", "http://den.local:8640/link"},
		{"behind a trusted proxy on TLS", Setup{}, "10.0.0.2:5000", "https", "https://den.local:8640/link"},
		{"told TLS by anyone else", Setup{}, "192.0.2.1:5000", "https", "http://den.local:8640/link"},
	} {
		api := New(slog.New(slog.DiscardHandler), domain.Info{}, Services{
			Auth: fakeAuth{}, Limits: &fakeLimiter{}, Setup: tc.setup, TrustedProxies: proxy,
		})
		req := httptest.NewRequest(http.MethodPost, "http://den.local:8640/api/v1/auth/device/start", strings.NewReader(`{"device": "TV", "client": "Photon"}`))
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
		if _, err := ParsePublicURL(bad); err == nil {
			t.Errorf("PHOTON_PUBLIC_URL=%q taken", bad)
		}
	}
}
