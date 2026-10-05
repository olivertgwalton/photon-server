package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// fakeHistory answers one play of Heat for whichever profile is asked about.
type fakeHistory struct{ asked []uuid.UUID }

func (f *fakeHistory) History(_ context.Context, profile uuid.UUID, _, _ int) ([]store.Play, int64, error) {
	f.asked = append(f.asked, profile)
	return []store.Play{{ID: uuid.NewV7(), Profile: profile, Card: store.Card{ID: films, Kind: domain.ItemMovie, Title: "Heat"}, Method: domain.PlayDirect}}, 1, nil
}

func TestHistoryIsAProfilesOwnAndAnAdminsWhole(t *testing.T) {
	h := &fakeHistory{}
	api := New(slog.New(slog.DiscardHandler), Info{}, Services{Auth: fakeAuth{}, History: h})
	for _, tc := range []struct {
		token, target string
		want          int
	}{
		{goodToken, "/api/v1/history", http.StatusOK},
		{goodToken, "/api/v1/admin/history", http.StatusOK},
		{goodToken, "/api/v1/admin/history?profile=" + oliver.ID.String(), http.StatusOK},
		{memberToken, "/api/v1/admin/history", http.StatusForbidden},
		{goodToken, "/api/v1/history?limit=0", http.StatusBadRequest},
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
