package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func newAPI(readiness error) *API {
	info := Info{ID: "0199b3c0-0000-7000-8000-000000000000", Name: "den", Version: "v0.1.0"}
	return New(slog.New(slog.DiscardHandler), info, Services{
		Ready:     func(context.Context) error { return readiness },
		Auth:      fakeAuth{},
		Limits:    &fakeLimiter{},
		Catalogue: fakeCatalogue{},
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
	var got Info
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(Info{ID: "0199b3c0-0000-7000-8000-000000000000", Name: "den", Version: "v0.1.0"}, got); diff != "" {
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
