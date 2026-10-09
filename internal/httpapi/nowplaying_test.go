//go:build integration

package httpapi

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/kv"
)

// A playback whose record lapsed in Valkey, with no node left to sweep it, is not on the
// dashboard: it shows no card for a playback that is gone.
func TestTheDashboardShowsNoLapsedPlayback(t *testing.T) {
	k, err := kv.Open(os.Getenv("TEST_VALKEY_URL"), uuid.NewV7())
	if err != nil {
		t.Fatalf("TEST_VALKEY_URL: %v", err)
	}
	t.Cleanup(k.Close)
	ctx := t.Context()
	title := func(name string) domain.Playback {
		return domain.Playback{ID: uuid.NewV7(), Card: domain.PlaybackCard{Title: domain.PlaybackTitle{ID: uuid.NewV7(), Title: name}}}
	}
	heat := title("Heat")
	if err := k.SavePlayback(ctx, heat, 200*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if err := k.SavePlayback(ctx, title("Ronin"), time.Minute); err != nil {
		t.Fatal(err)
	}
	// Valkey lapses it on its own clock.
	for lapsed := false; !lapsed; {
		select {
		case <-ctx.Done():
			t.Fatal("it never lapsed")
		case <-time.After(50 * time.Millisecond):
		}
		_, there, err := k.Playback(ctx, heat.ID)
		lapsed = !there && err == nil
	}
	api := New(slog.New(slog.DiscardHandler), domain.Info{}, Services{Copies: noCopies{}, Discover: noDiscoveries{}, Auth: fakeAuth{}, NowPlaying: k, HLS: fakeHLS{}})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/playbacks", nil)
	req.Header.Set("Authorization", "Bearer "+goodToken)
	rec := httptest.NewRecorder()
	api.ServeHTTP(rec, req)
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, "Ronin") || strings.Contains(body, "Heat") || strings.Count(body, `"title":{`) != 1 {
		t.Errorf("dashboard = %d %s; want Ronin alone", rec.Code, body)
	}
}
