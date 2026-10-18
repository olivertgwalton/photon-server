package httpapi

import (
	"context"
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

// fakeStatistics answers one play of Heat, and keeps what each report was asked over.
type fakeStatistics struct {
	days  []int
	zones []string
}

func (f *fakeStatistics) Statistics(_ context.Context, days int, zone *time.Location, _ uuid.UUID) (store.Statistics, error) {
	f.days, f.zones = append(f.days, days), append(f.zones, zone.String())
	watched := store.Watched{Plays: 1, WatchedMS: 7_200_000}
	return store.Statistics{
		Watched:  watched,
		Profiles: []store.ProfileWatched{{Profile: oliver.ID, Name: "Oliver", Watched: watched}},
		Titles:   []store.TitleWatched{{Card: store.Card{ID: films, Kind: domain.ItemMovie, Title: "Heat"}, Watched: watched}},
		Days:     []store.DayWatched{{Day: domain.Date(time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)), Watched: watched}},
	}, nil
}

func TestAnAdminReadsWhatWasWatched(t *testing.T) {
	f := &fakeStatistics{}
	api := New(slog.New(slog.DiscardHandler), domain.Info{}, Services{Auth: fakeAuth{}, Statistics: f})
	for _, tc := range []struct {
		token, target string
		want          int
		has           string
	}{
		{goodToken, "/api/v1/admin/statistics", http.StatusOK, `"days":30,"zone":"UTC","plays":1,"watched_ms":7200000`},
		{goodToken, "/api/v1/admin/statistics?days=7&zone=Europe/London", http.StatusOK, `"zone":"Europe/London"`},
		{goodToken, "/api/v1/admin/statistics", http.StatusOK, `"profiles":[{"profile_id":"` + oliver.ID.String() + `","name":"Oliver"`},
		{goodToken, "/api/v1/admin/statistics", http.StatusOK, `"titles":[{"title":{"id":"` + films.String()},
		{goodToken, "/api/v1/admin/statistics", http.StatusOK, `"daily":[{"day":"2026-10-18","plays":1`},
		{memberToken, "/api/v1/admin/statistics", http.StatusForbidden, ""},
		{goodToken, "/api/v1/admin/statistics?days=0", http.StatusBadRequest, "days is a number from 1 to 3650"},
		{goodToken, "/api/v1/admin/statistics?zone=Mars/Olympus", http.StatusBadRequest, "time zone"},
		{goodToken, "/api/v1/admin/statistics?zone=Local", http.StatusBadRequest, "time zone"},
		{goodToken, "/api/v1/admin/statistics?profile=oliver", http.StatusBadRequest, "profile is not an id"},
	} {
		req := httptest.NewRequest(http.MethodGet, tc.target, nil)
		req.Header.Set("Authorization", "Bearer "+tc.token)
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		if rec.Code != tc.want || !strings.Contains(rec.Body.String(), tc.has) {
			t.Errorf("%s: %d %s, want %d with %s", tc.target, rec.Code, rec.Body, tc.want, tc.has)
		}
	}
	// A report is a month in UTC unless it asks for another span or zone.
	if f.days[0] != 30 || f.zones[0] != "UTC" || f.days[1] != 7 || f.zones[1] != "Europe/London" {
		t.Errorf("asked over %v days in %v, want 30 in UTC, then 7 in Europe/London", f.days, f.zones)
	}
}
