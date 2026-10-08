package main

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A node's metrics say which version it runs, beside the Go runtime's.
func TestMetricsSayTheVersionAndTheRuntime(t *testing.T) {
	rec := httptest.NewRecorder()
	metricsHandler(metrics("v1.2.3"), slog.New(slog.DiscardHandler)).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	for _, want := range []string{`photon_build_info{version="v1.2.3"} 1`, "go_goroutines "} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("no %q in:\n%s", want, rec.Body)
		}
	}
}
