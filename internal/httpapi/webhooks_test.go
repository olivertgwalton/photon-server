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

type fakeWebhooks struct{ added []store.Webhook }

func (f *fakeWebhooks) Webhooks(context.Context) ([]store.Webhook, error) { return f.added, nil }

func (f *fakeWebhooks) AddWebhook(_ context.Context, url string, kinds []domain.EventKind, _ string) (store.Webhook, error) {
	h := store.Webhook{ID: uuid.NewV7(), URL: url, Events: kinds, CreatedAt: time.Now()}
	f.added = append(f.added, h)
	return h, nil
}

func (f *fakeWebhooks) RemoveWebhook(_ context.Context, id uuid.UUID) error {
	for i, h := range f.added {
		if h.ID == id {
			f.added = append(f.added[:i], f.added[i+1:]...)
			return nil
		}
	}
	return store.ErrNotFound
}

func TestAnAdminKeepsTheWebhooks(t *testing.T) {
	hooks := &fakeWebhooks{}
	told := &fakeEvents{}
	api := New(slog.New(slog.DiscardHandler), Info{}, Services{Auth: fakeAuth{}, Webhooks: hooks, Events: told})
	do := func(token, method, target, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		return rec
	}
	rec := do(goodToken, http.MethodPost, "/api/v1/admin/webhooks", `{"url": "https://hooks.example/photon", "events": ["playback.started", "playback.paused"]}`)
	var added addedWebhookJSON
	if err := json.NewDecoder(rec.Body).Decode(&added); err != nil || rec.Code != http.StatusCreated || len(added.Secret) < 20 {
		t.Fatalf("adding: %d %+v %v; want it with its secret", rec.Code, added, err)
	}
	told.webhooks = []uuid.UUID{added.ID}
	hook := "/api/v1/admin/webhooks/" + added.ID.String()
	for _, tc := range []struct {
		token, method, target, body string
		want                        int
	}{
		{memberToken, http.MethodGet, "/api/v1/admin/webhooks", "", http.StatusForbidden},
		{goodToken, http.MethodPost, "/api/v1/admin/webhooks", `{"url": "ftp://hooks.example", "events": ["playback.started"]}`, http.StatusBadRequest},
		{goodToken, http.MethodPost, "/api/v1/admin/webhooks", `{"url": "https://hooks.example", "events": []}`, http.StatusBadRequest},
		{goodToken, http.MethodPost, "/api/v1/admin/webhooks", `{"url": "https://hooks.example", "events": ["job.finished"]}`, http.StatusBadRequest},
		{goodToken, http.MethodPost, "/api/v1/admin/webhooks", `{"url": "https://hooks.example", "events": ["auth.signed_in", "auth.signed_in"]}`, http.StatusBadRequest},
		{goodToken, http.MethodPost, hook + "/test", "", http.StatusAccepted},
		{goodToken, http.MethodPost, "/api/v1/admin/webhooks/" + uuid.NewV7().String() + "/test", "", http.StatusNotFound},
	} {
		if rec := do(tc.token, tc.method, tc.target, tc.body); rec.Code != tc.want {
			t.Errorf("%s %s %s: %d, want %d: %s", tc.method, tc.target, tc.body, rec.Code, tc.want, rec.Body)
		}
	}
	listed := do(goodToken, http.MethodGet, "/api/v1/admin/webhooks", "")
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), `"events":["playback.started","playback.paused"]`) || strings.Contains(listed.Body.String(), added.Secret) {
		t.Errorf("listing: %d %s; want the webhook without its secret", listed.Code, listed.Body)
	}
	if rec := do(goodToken, http.MethodDelete, hook, ""); rec.Code != http.StatusNoContent {
		t.Errorf("removing: %d", rec.Code)
	}
	if rec := do(goodToken, http.MethodDelete, hook, ""); rec.Code != http.StatusNotFound {
		t.Errorf("removing again: %d, want 404", rec.Code)
	}
}
