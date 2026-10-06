package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

type memoryMaintenance struct{ m domain.Maintenance }

func (s *memoryMaintenance) Maintenance(context.Context) (domain.Maintenance, error) { return s.m, nil }

func (s *memoryMaintenance) SetMaintenance(_ context.Context, m domain.Maintenance) error {
	s.m = m
	return nil
}

func TestAnAdminSetsTheMaintenanceWindow(t *testing.T) {
	settings := &memoryMaintenance{domain.Maintenance{StartHour: 2, EndHour: 5, Zone: time.UTC, Previews: domain.TimingWindow, Markers: domain.TimingWindowAndAdded}}
	api := New(slog.New(slog.DiscardHandler), domain.Info{}, Services{Auth: fakeAuth{}, Maintenance: settings})
	do := func(token, method, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/api/v1/admin/maintenance", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		return rec
	}
	if rec := do(memberToken, http.MethodGet, ""); rec.Code != http.StatusForbidden {
		t.Errorf("a member: %d, want 403", rec.Code)
	}
	if rec := do(goodToken, http.MethodGet, ""); !strings.Contains(rec.Body.String(),
		`{"start_hour":2,"end_hour":5,"time_zone":"UTC","previews":"window","markers":"window_and_added"}`) {
		t.Errorf("GET: %d %s", rec.Code, rec.Body)
	}
	// A window past midnight, in the household's own zone.
	overnight := `{"start_hour":23,"end_hour":6,"time_zone":"Europe/London","previews":"window_and_added","markers":"window"}`
	if rec := do(goodToken, http.MethodPut, overnight); rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != overnight {
		t.Errorf("PUT: %d %s", rec.Code, rec.Body)
	}
	if rec := do(goodToken, http.MethodGet, ""); strings.TrimSpace(rec.Body.String()) != overnight {
		t.Errorf("GET after PUT: %s, want %s", rec.Body, overnight)
	}
	for name, body := range map[string]string{
		"no hours":           `{"start_hour":3,"end_hour":3,"time_zone":"UTC","previews":"window","markers":"window"}`,
		"an hour past a day": `{"start_hour":22,"end_hour":24,"time_zone":"UTC","previews":"window","markers":"window"}`,
		"an unknown zone":    `{"start_hour":2,"end_hour":5,"time_zone":"Mars/Olympus","previews":"window","markers":"window"}`,
		"the server's zone":  `{"start_hour":2,"end_hour":5,"time_zone":"Local","previews":"window","markers":"window"}`,
		"no timing":          `{"start_hour":2,"end_hour":5,"time_zone":"UTC","previews":"window"}`,
		"an unknown timing":  `{"start_hour":2,"end_hour":5,"time_zone":"UTC","previews":"never","markers":"window"}`,
	} {
		if rec := do(goodToken, http.MethodPut, body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s, want 400", name, rec.Code, rec.Body)
		}
	}
	if settings.m.StartHour != 23 {
		t.Errorf("a refused change was kept: %+v", settings.m)
	}
}
