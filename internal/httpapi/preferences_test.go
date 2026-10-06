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
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// fakePreferences keeps each profile's preferences in memory, and remembers no tracks.
type fakePreferences struct {
	mu   sync.Mutex
	kept map[uuid.UUID]domain.Preferences
}

func (f *fakePreferences) Preferences(_ context.Context, profile uuid.UUID) (domain.Preferences, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if p, ok := f.kept[profile]; ok {
		return p, nil
	}
	return domain.DefaultPreferences(), nil
}

func (f *fakePreferences) SetPreferences(_ context.Context, profile uuid.UUID, p domain.Preferences) (domain.Preferences, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.kept == nil {
		f.kept = map[uuid.UUID]domain.Preferences{}
	}
	p.SavedAt = time.Now()
	f.kept[profile] = p
	return p, nil
}

func (*fakePreferences) ChosenTracks(context.Context, uuid.UUID, uuid.UUID) (domain.ChosenTracks, error) {
	return domain.ChosenTracks{}, nil
}

func TestAProfileKeepsHowItPlays(t *testing.T) {
	api := New(slog.New(slog.DiscardHandler), domain.Info{}, Services{Auth: fakeAuth{}, Preferences: &fakePreferences{}})
	call := func(method, body string) (int, string) {
		req := httptest.NewRequest(method, "/api/v1/me/preferences", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+goodToken)
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		return rec.Code, rec.Body.String()
	}

	// Until it changes anything, a profile plays as Jellyfin's do, and says it has saved nothing.
	code, body := call(http.MethodGet, "")
	if code != http.StatusOK || !strings.Contains(body, `"subtitle_mode":"default"`) || !strings.Contains(body, `"next_episode":"play"`) ||
		strings.Contains(body, "saved_at") {
		t.Fatalf("GET = %d %s", code, body)
	}

	code, body = call(http.MethodPatch, `{"subtitle_language": "fr", "subtitle_mode": "smart", "max_bitrate_kbps": 8000}`)
	if code != http.StatusOK || !strings.Contains(body, `"subtitle_language":"fr"`) || !strings.Contains(body, `"saved_at"`) {
		t.Fatalf("PATCH = %d %s", code, body)
	}
	// What a change leaves out stays.
	if _, body = call(http.MethodPatch, `{"intro_action": "skip"}`); !strings.Contains(body, `"subtitle_mode":"smart"`) ||
		!strings.Contains(body, `"max_bitrate_kbps":8000`) || !strings.Contains(body, `"intro_action":"skip"`) {
		t.Errorf("a second change lost the first: %s", body)
	}
	if _, body = call(http.MethodPatch, `{"subtitle_language": ""}`); !strings.Contains(body, `"subtitle_language":""`) {
		t.Errorf("an empty language is any: %s", body)
	}

	if _, body = call(http.MethodPatch, `{"home": [{"row": "next_up", "visibility": "shown"}, {"row": "favourites", "visibility": "hidden"}]}`); !strings.Contains(body, `"home":[{"row":"next_up","visibility":"shown"},{"row":"favourites","visibility":"hidden"}`) {
		t.Errorf("the home arranged: %s", body)
	}

	for _, bad := range []string{
		`{"subtitle_mode": "foreign"}`, `{"audio_language": "not a language"}`, `{"max_bitrate_kbps": -1}`,
		`{"home": [{"row": "next_up", "visibility": "shown"}, {"row": "next_up", "visibility": "hidden"}]}`,
		`{"home": [{"row": "next_up"}]}`, `{"home": [{"row": "trending", "visibility": "shown"}]}`,
	} {
		if code, body := call(http.MethodPatch, bad); code != http.StatusBadRequest {
			t.Errorf("PATCH %s = %d %s, want 400", bad, code, body)
		}
	}
}
