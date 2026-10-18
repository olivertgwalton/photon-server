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
	since []time.Time
	zones []string
}

func (f *fakeStatistics) Statistics(_ context.Context, since time.Time, zone *time.Location, _ uuid.UUID) (store.Statistics, error) {
	f.since, f.zones = append(f.since, since), append(f.zones, zone.String())
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
	// A week's report starts at midnight six days back where it is read, so today is its seventh.
	london, err := time.LoadLocation("Europe/London")
	if err != nil {
		t.Fatal(err)
	}
	week := f.since[1].In(london)
	if f.zones[1] != "Europe/London" || week.Hour() != 0 || week.Minute() != 0 ||
		time.Now().In(london).Sub(week) < 6*24*time.Hour || time.Now().In(london).Sub(week) > 7*24*time.Hour+time.Hour {
		t.Errorf("a week's report was asked from %s in %s, want midnight six days back in London", week, f.zones[1])
	}
}
