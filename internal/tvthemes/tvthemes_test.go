package tvthemes

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/artwork"
	"github.com/olivertgwalton/photon-server/internal/kv"
)

type fakeStore struct {
	tvdb  string
	saved map[uuid.UUID]string
}

func (s *fakeStore) ThemeSubject(context.Context, uuid.UUID) (string, bool, error) {
	return s.tvdb, len(s.saved) == 0, nil
}

func (s *fakeStore) SaveFetchedTheme(_ context.Context, _, id uuid.UUID, url string) error {
	s.saved[id] = url
	return nil
}

type fakeMisses map[string]time.Duration

func (fakeMisses) Allow(context.Context, string, kv.Limit) (time.Duration, error) { return 0, nil }

func (m fakeMisses) NoteThemeMissing(_ context.Context, tvdb string, ttl time.Duration) error {
	m[tvdb] = ttl
	return nil
}

func (m fakeMisses) ThemeMissing(_ context.Context, tvdb string) (bool, error) {
	_, ok := m[tvdb]
	return ok, nil
}

// The host's theme is kept in the cache and recorded; a show it has none for is asked about once.
func TestAThemeIsKeptAndAMissingOneAskedOnce(t *testing.T) {
	var asked atomic.Int32
	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked.Add(1)
		if r.URL.Path != "/81189.mp3" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = io.WriteString(w, "ID3 breaking bad")
	}))
	defer host.Close()
	cache, err := artwork.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Close()
	misses := fakeMisses{}

	found := &fakeStore{tvdb: "81189", saved: map[uuid.UUID]string{}}
	if err := Fetch(found, cache, misses, host.URL+"/")(t.Context(), uuid.NewV7()); err != nil {
		t.Fatal(err)
	}
	if len(found.saved) != 1 {
		t.Fatalf("saved %v, want the one theme", found.saved)
	}
	for id, url := range found.saved {
		f, err := cache.Sound(t.Context(), id, url)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(f)
		f.Close()
		if string(body) != "ID3 breaking bad" || url != host.URL+"/81189.mp3" {
			t.Errorf("kept %q from %s, want the host's tune", body, url)
		}
	}

	none := &fakeStore{tvdb: "1", saved: map[uuid.UUID]string{}}
	for range 2 {
		if err := Fetch(none, cache, misses, host.URL+"/")(t.Context(), uuid.NewV7()); err != nil {
			t.Fatal(err)
		}
	}
	if len(none.saved) != 0 || misses["1"] != missFor || asked.Load() != 2 {
		t.Errorf("a show the host has no theme for: saved %v, noted %v, host asked %d times; want nothing, a month, once", none.saved, misses, asked.Load()-1)
	}
}
