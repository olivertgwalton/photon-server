package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// fakeHistory answers one play of Heat for whichever profile is asked about, and that there are
// more from the first page.
type fakeHistory struct {
	asked  []uuid.UUID
	before []store.Before
}

var heatPlayed = time.Date(2026, 10, 1, 20, 0, 0, 123456000, time.UTC)

func (f *fakeHistory) History(_ context.Context, profile uuid.UUID, before store.Before, _ int) ([]store.Play, bool, error) {
	f.asked, f.before = append(f.asked, profile), append(f.before, before)
	play := store.Play{ID: films, Profile: profile, Card: store.Card{ID: films, Kind: domain.ItemMovie, Title: "Heat"}, Method: domain.PlayDirect, StoppedAt: heatPlayed}
	return []store.Play{play}, before == store.Newest, nil
}

func TestHistoryIsAProfilesOwnAndAnAdminsWhole(t *testing.T) {
	h := &fakeHistory{}
	api := New(slog.New(slog.DiscardHandler), domain.Info{}, Services{Auth: fakeAuth{}, History: h})
	for _, tc := range []struct {
		token, target string
		want          int
	}{
		{goodToken, "/api/v1/history", http.StatusOK},
		{goodToken, "/api/v1/admin/history", http.StatusOK},
		{goodToken, "/api/v1/admin/history?profile=" + oliver.ID.String(), http.StatusOK},
		{memberToken, "/api/v1/admin/history", http.StatusForbidden},
		{goodToken, "/api/v1/history?limit=0", http.StatusBadRequest},
		{goodToken, "/api/v1/admin/history?profile=oliver", http.StatusBadRequest},
	} {
		req := httptest.NewRequest(http.MethodGet, tc.target, nil)
		req.Header.Set("Authorization", "Bearer "+tc.token)
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		if rec.Code != tc.want || (tc.want == http.StatusOK && !strings.Contains(rec.Body.String(), `"title":{"id":"`+films.String())) {
			t.Errorf("%s: %d %s, want %d", tc.target, rec.Code, rec.Body, tc.want)
		}
	}
	if len(h.asked) != 3 || h.asked[0] != oliver.ID || h.asked[1] != (uuid.UUID{}) || h.asked[2] != oliver.ID {
		t.Errorf("asked about %v, want Oliver's own, everyone's, then Oliver's", h.asked)
	}
}

func TestHistoryIsPagedOnFromThePageBefore(t *testing.T) {
	h := &fakeHistory{}
	api := New(slog.New(slog.DiscardHandler), domain.Info{}, Services{Auth: fakeAuth{}, History: h})
	get := func(target string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		req.Header.Set("Authorization", "Bearer "+goodToken)
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		return rec
	}
	var first struct{ Next string }
	if rec := get("/api/v1/history"); rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &first) != nil || first.Next == "" {
		t.Fatalf("first page: %d %s, want a next", rec.Code, rec.Body)
	}
	var second map[string]any
	if rec := get("/api/v1/history?after=" + first.Next); rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &second) != nil || second["next"] != nil {
		t.Fatalf("second page: %d %s, want the last, with no next", rec.Code, rec.Body)
	}
	if want := (store.Before{At: heatPlayed, ID: films}); len(h.before) != 2 || h.before[0] != store.Newest || !h.before[1].At.Equal(want.At) || h.before[1].ID != want.ID {
		t.Errorf("asked from %v, want the newest, then from the first page's last play", h.before)
	}
	for _, target := range []string{"/api/v1/history?after=nonsense", "/api/v1/history?offset=50"} {
		if rec := get(target); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d, want 400", target, rec.Code)
		}
	}
}
