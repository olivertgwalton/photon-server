package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/historyimport"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// fakeImports takes only the token "good", as a Plex server would.
type fakeImports struct{ started []domain.HistoryImport }

func (f *fakeImports) Start(_ context.Context, kind domain.ImportSource, address string, profile uuid.UUID, c historyimport.Credentials) (uuid.UUID, error) {
	if c.Token != "good" {
		return uuid.UUID{}, fmt.Errorf("%w: the plex server refused the credentials", historyimport.ErrRefused)
	}
	h := domain.HistoryImport{ID: uuid.NewV7(), Source: kind, URL: address, Profile: profile, Status: domain.ImportQueued, CreatedAt: time.Now()}
	f.started = append(f.started, h)
	return h.ID, nil
}

func (f *fakeImports) Imports(context.Context) ([]domain.HistoryImport, error) { return f.started, nil }

func (f *fakeImports) Import(_ context.Context, id uuid.UUID) (domain.HistoryImport, error) {
	for _, h := range f.started {
		if h.ID == id {
			return h, nil
		}
	}
	return domain.HistoryImport{}, store.ErrNotFound
}

func TestAnAdminImportsWatchHistory(t *testing.T) {
	imports := &fakeImports{}
	api := New(slog.New(slog.DiscardHandler), domain.Info{}, Services{Auth: fakeAuth{}, Importer: imports, HistoryImports: imports})
	do := func(token, method, target, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		return rec
	}
	start := func(source, token string) string {
		return `{"source": "` + source + `", "url": "http://plex.lan:32400", "profile_id": "` + oliver.ID.String() +
			`", "credentials": {"token": "` + token + `"}}`
	}
	for _, tc := range []struct {
		token, body string
		want        int
		says        string
	}{
		{memberToken, start("plex", "good"), http.StatusForbidden, ""},
		{goodToken, start("kodi", "good"), http.StatusBadRequest, "source"},
		{goodToken, start("plex", "bad"), http.StatusBadRequest, "refused the credentials"},
	} {
		rec := do(tc.token, http.MethodPost, "/api/v1/admin/imports", tc.body)
		if rec.Code != tc.want || !strings.Contains(rec.Body.String(), tc.says) {
			t.Errorf("%s: %d %s, want %d saying %q", tc.body, rec.Code, rec.Body, tc.want, tc.says)
		}
	}
	rec := do(goodToken, http.MethodPost, "/api/v1/admin/imports", start("plex", "good"))
	var created createdJSON
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil || rec.Code != http.StatusCreated {
		t.Fatalf("starting: %d %v; want its id", rec.Code, err)
	}
	got := do(goodToken, http.MethodGet, "/api/v1/admin/imports/"+created.ID.String(), "")
	if got.Code != http.StatusOK || !strings.Contains(got.Body.String(), `"status":"queued"`) || strings.Contains(got.Body.String(), "good") {
		t.Errorf("the import: %d %s; want it queued, without its token", got.Code, got.Body)
	}
	if listed := do(goodToken, http.MethodGet, "/api/v1/admin/imports", ""); listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), created.ID.String()) {
		t.Errorf("listing: %d %s; want the import", listed.Code, listed.Body)
	}
	if rec := do(goodToken, http.MethodGet, "/api/v1/admin/imports/"+uuid.NewV7().String(), ""); rec.Code != http.StatusNotFound {
		t.Errorf("an unknown import: %d, want 404", rec.Code)
	}
}
