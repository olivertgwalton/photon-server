package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/olivertgwalton/photon-server/internal/auth"
	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/kv"
)

// fakeLimiter allows each key Burst attempts and records every key asked about.
type fakeLimiter struct {
	mu   sync.Mutex
	used map[string]int
	keys []string
}

func (f *fakeLimiter) Allow(_ context.Context, key string, l kv.Limit) (time.Duration, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.used == nil {
		f.used = map[string]int{}
	}
	f.keys = append(f.keys, key)
	f.used[key]++
	if f.used[key] > l.Burst {
		return 42 * time.Second, nil
	}
	return 0, nil
}

func TestSignInsAreLimitedByAddressAndName(t *testing.T) {
	limits := &fakeLimiter{}
	api := New(slog.New(slog.DiscardHandler), domain.Info{}, Services{Auth: fakeAuth{}, Limits: limits, Events: &fakeEvents{}})
	attempt := func(peer, forwarded string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login",
			strings.NewReader(`{"name":"Oliver","password":"guess","device":"d","client":"c"}`))
		req.RemoteAddr = peer
		req.Header.Set("X-Forwarded-For", forwarded)
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		return rec
	}
	for range auth.SignInsPerAddress.Burst {
		attempt("203.0.113.9:5000", "")
	}
	rec := attempt("203.0.113.9:5000", "198.51.100.1")
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") != "42" {
		t.Errorf("past the burst, with a forged X-Forwarded-For: %d, Retry-After %q; want 429, 42",
			rec.Code, rec.Header().Get("Retry-After"))
	}
	for _, k := range limits.keys {
		if strings.Contains(k, "198.51.100.1") {
			t.Errorf("an untrusted client's X-Forwarded-For chose the limit key %q", k)
		}
	}
	if limits.used["signin:name:oliver"] == 0 {
		t.Error("attempts were not counted against the name")
	}
}
