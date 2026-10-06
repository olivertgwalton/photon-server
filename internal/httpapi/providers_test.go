package httpapi

import (
	"context"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/provider"
)

// keyed is a rater that needs a key.
type keyed struct{}

func (keyed) Info() provider.Info {
	return provider.Info{
		ID: domain.SourceMDBList, Name: "MDBList", Kinds: []domain.ItemKind{domain.ItemMovie},
		Settings: []provider.Setting{{Key: "api_key", Name: "API key", Secret: true, Required: true}},
	}
}

func (keyed) Ratings(context.Context, domain.ItemKind, map[domain.Provider]string) ([]domain.Rating, error) {
	return nil, nil
}

type memorySettings map[domain.FieldSource]map[string]string

func (m memorySettings) ProviderSettings(_ context.Context, id domain.FieldSource) (map[string]string, error) {
	return m[id], nil
}

func (m memorySettings) SetProviderSettings(_ context.Context, id domain.FieldSource, change map[string]string) error {
	if m[id] == nil {
		m[id] = map[string]string{}
	}
	maps.Copy(m[id], change)
	return nil
}

func TestAnAdminSetsAProvidersKeyAndNeverSeesItAgain(t *testing.T) {
	settings := memorySettings{}
	api := New(slog.New(slog.DiscardHandler), domain.Info{}, Services{
		Auth: fakeAuth{}, Providers: provider.NewRegistry(nil, keyed{}), ProviderSettings: settings,
	})
	do := func(token, method, target, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		return rec
	}
	if rec := do(memberToken, http.MethodGet, "/api/v1/admin/providers", ""); rec.Code != http.StatusForbidden {
		t.Errorf("a member: %d, want 403", rec.Code)
	}
	if rec := do(goodToken, http.MethodGet, "/api/v1/admin/providers", ""); !strings.Contains(rec.Body.String(),
		`"capabilities":["rate"],"settings":[{"key":"api_key","name":"API key","secret":true,"required":true,"set":false}],"ready":false`) {
		t.Errorf("before a key: %s", rec.Body)
	}
	if rec := do(goodToken, http.MethodPatch, "/api/v1/admin/providers/mdblist", `{"settings": {"colour": "red"}}`); rec.Code != http.StatusBadRequest {
		t.Errorf("a setting it does not have: %d, want 400", rec.Code)
	}
	if rec := do(goodToken, http.MethodPatch, "/api/v1/admin/providers/omdb", `{"settings": {}}`); rec.Code != http.StatusNotFound {
		t.Errorf("a provider it does not have: %d, want 404", rec.Code)
	}
	rec := do(goodToken, http.MethodPatch, "/api/v1/admin/providers/mdblist", `{"settings": {"api_key": "s3cret"}}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"set":true}],"ready":true`) || strings.Contains(rec.Body.String(), "s3cret") {
		t.Errorf("after setting the key: %d %s; want it ready, and the key not sent back", rec.Code, rec.Body)
	}
	if settings[domain.SourceMDBList]["api_key"] != "s3cret" {
		t.Errorf("kept %v", settings)
	}
}
