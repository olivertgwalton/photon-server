package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

func newAPI(readiness error) *API {
	info := domain.Info{ID: "0199b3c0-0000-7000-8000-000000000000", Name: "den", Version: "v0.1.0"}
	return New(slog.New(slog.DiscardHandler), info, Services{
		Ready:     func(context.Context) error { return readiness },
		Auth:      fakeAuth{},
		Limits:    &fakeLimiter{},
		Catalogue: fakeCatalogue{},
		People:    &fakePeople{},
		Watching:  fakeWatching{},
		Events:    &fakeEvents{},
	})
}

func TestServer(t *testing.T) {
	rec := httptest.NewRecorder()
	newAPI(nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/server", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	var got domain.Info
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(domain.Info{ID: "0199b3c0-0000-7000-8000-000000000000", Name: "den", Version: "v0.1.0"}, got); diff != "" {
		t.Errorf("body (-want +got):\n%s", diff)
	}
}

func TestRefusals(t *testing.T) {
	tests := []struct {
		name      string
		method    string
		target    string
		want      problem
		wantAllow string
	}{
		{
			name:   "unknown path",
			method: http.MethodGet,
			target: "/api/v1/nothing",
			want:   problem{Title: "Not Found", Status: http.StatusNotFound, Code: codeNotFound},
		},
		{
			name:   "wrong method",
			method: http.MethodPost,
			target: "/api/v1/server",
			want: problem{
				Title:  "Method Not Allowed",
				Status: http.StatusMethodNotAllowed,
				Code:   codeMethodNotAllowed,
			},
			wantAllow: "GET, HEAD",
		},
		{
			name:   "unknown query parameter",
			method: http.MethodGet,
			target: "/api/v1/server?api_key=x",
			want: problem{
				Title:  "Bad Request",
				Status: http.StatusBadRequest,
				Code:   codeUnknownParameter,
				Detail: "api_key",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			newAPI(nil).ServeHTTP(rec, httptest.NewRequest(tt.method, tt.target, nil))

			res := rec.Result()
			if res.StatusCode != tt.want.Status {
				t.Errorf("status = %d, want %d", res.StatusCode, tt.want.Status)
			}
			if got := res.Header.Get("Content-Type"); got != "application/problem+json" {
				t.Errorf("Content-Type = %q, want application/problem+json", got)
			}
			if got := res.Header.Get("Allow"); got != tt.wantAllow {
				t.Errorf("Allow = %q, want %q", got, tt.wantAllow)
			}
			body, err := io.ReadAll(res.Body)
			if err != nil {
				t.Fatal(err)
			}
			var got problem
			if err := json.Unmarshal(body, &got); err != nil {
				t.Fatalf("body %q: %v", body, err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("problem (-want +got):\n%s", diff)
			}
		})
	}
}

func TestReadyz(t *testing.T) {
	tests := []struct {
		name      string
		readiness error
		want      int
	}{
		{name: "dependencies reachable", want: http.StatusNoContent},
		{name: "a dependency down", readiness: errors.New("valkey: connection refused"), want: http.StatusServiceUnavailable},
		// A node draining as it stops is sent no new clients by a balancer that asks.
		{name: "the node stopping", readiness: domain.ErrStopping, want: http.StatusServiceUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			newAPI(tt.readiness).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
			if rec.Code != tt.want {
				t.Errorf("status = %d, want %d", rec.Code, tt.want)
			}
			if body := rec.Body.String(); tt.readiness != nil && strings.Contains(body, "connection refused") {
				t.Errorf("body %q leaks the dependency's error to an unauthenticated caller", body)
			}
		})
	}
}

type servingAs domain.SecureConnections

func (s servingAs) Mode() domain.SecureConnections { return domain.SecureConnections(s) }

// Required sends a plain request to HTTPS, but one from this machine or forwarded as HTTPS by a
// trusted proxy.
func TestRequiredSecureConnectionsSendPlainRequestsToHTTPS(t *testing.T) {
	api := New(slog.New(slog.DiscardHandler), domain.Info{}, Services{
		Ready: func(context.Context) error { return nil }, Secure: servingAs(domain.SecureRequired),
		TrustedProxies: []netip.Prefix{netip.MustParsePrefix("192.168.86.81/32")},
	})
	for _, tc := range []struct {
		peer, proto string
		want        int
	}{
		{"192.168.86.20:5000", "", http.StatusTemporaryRedirect},
		{"192.168.86.20:5000", "https", http.StatusTemporaryRedirect},
		{"127.0.0.1:5000", "", http.StatusNoContent},
		{"192.168.86.81:5000", "https", http.StatusNoContent},
	} {
		r := httptest.NewRequest(http.MethodGet, "http://mini.local:8640/readyz", nil)
		r.RemoteAddr = tc.peer
		if tc.proto != "" {
			r.Header.Set("X-Forwarded-Proto", tc.proto)
		}
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, r)
		if rec.Code != tc.want {
			t.Errorf("from %s, forwarded %q: status %d, want %d", tc.peer, tc.proto, rec.Code, tc.want)
		}
		if loc := rec.Header().Get("Location"); tc.want == http.StatusTemporaryRedirect && loc != "https://mini.local:8640/readyz" {
			t.Errorf("from %s: sent to %q", tc.peer, loc)
		}
	}
}
