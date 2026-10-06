package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
)

var (
	localTheme   = uuid.MustParse("0199b3c0-0000-7000-8000-0000000000b1")
	fetchedTheme = uuid.MustParse("0199b3c0-0000-7000-8000-0000000000b2")
)

type fakeThemes struct{ root string }

func (f fakeThemes) Theme(_ context.Context, id uuid.UUID) (store.ThemeFile, error) {
	switch id {
	case localTheme:
		return store.ThemeFile{Source: domain.ThemeFromFile, Root: f.root, Path: "Heat (1995)/theme-music/Main Title.FLAC"}, nil
	case fetchedTheme:
		return store.ThemeFile{Source: domain.ThemeFromThemerr}, nil
	}
	return store.ThemeFile{}, store.ErrNotFound
}

// A player sends no token for a theme, names its type by its file, and reads it in ranges.
func TestTheme(t *testing.T) {
	root := t.TempDir()
	for name, body := range map[string]string{"Heat (1995)/theme-music/Main Title.FLAC": "local flac", "tune": "fetched m4a"} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	api := New(slog.New(slog.DiscardHandler), domain.Info{}, Services{
		Auth: fakeAuth{}, Themes: fakeThemes{root: root}, Artwork: fakePictures{root: root},
	})
	get := func(id uuid.UUID, ranged string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/themes/"+id.String(), nil)
		if ranged != "" {
			req.Header.Set("Range", ranged)
		}
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		return rec
	}
	for id, want := range map[uuid.UUID][2]string{localTheme: {"local flac", "audio/flac"}, fetchedTheme: {"fetched m4a", "audio/mp4"}} {
		rec := get(id, "")
		if rec.Code != http.StatusOK || rec.Body.String() != want[0] || rec.Header().Get("Content-Type") != want[1] {
			t.Errorf("%v: %d %q as %q, want 200 %q as %s", id, rec.Code, rec.Body.String(), rec.Header().Get("Content-Type"), want[0], want[1])
		}
	}
	if rec := get(fetchedTheme, "bytes=8-"); rec.Code != http.StatusPartialContent || rec.Body.String() != "m4a" {
		t.Errorf("a range: %d %q, want 206 %q", rec.Code, rec.Body.String(), "m4a")
	}
	if rec := get(uuid.NewV7(), ""); rec.Code != http.StatusNotFound {
		t.Errorf("an unknown theme: %d, want 404", rec.Code)
	}
}
