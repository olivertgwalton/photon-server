package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/playback"
	"github.com/olivertgwalton/photon-server/internal/store"
)

var previewedPart = uuid.MustParse("0199b3c0-0000-7000-8000-0000000000b1")

// fakePreviews has two sheets and a first chapter's image for one part, which only Oliver may see.
type fakePreviews struct{ dir string }

func (fakePreviews) Trickplay(_ context.Context, profile, part uuid.UUID) (store.Trickplay, error) {
	if profile != oliver.ID || part != previewedPart {
		return store.Trickplay{}, store.ErrNotFound
	}
	return store.Trickplay{Width: 320, Height: 180, IntervalMS: 10_000, Columns: 10, Rows: 10, Thumbnails: 105, Sheets: 2}, nil
}

func (f fakePreviews) Sheet(_ uuid.UUID, n int) (*os.File, error) {
	return os.Open(filepath.Join(f.dir, "sheet"+string(rune('0'+n))+".jpg"))
}

func (f fakePreviews) ChapterImage(_ uuid.UUID, idx int) (*os.File, error) {
	if idx != 0 {
		return nil, os.ErrNotExist
	}
	return os.Open(filepath.Join(f.dir, "chapter.jpg"))
}

func TestPreviewsAreServedToThoseWhoMaySeeTheTitle(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"sheet0.jpg", "sheet1.jpg", "chapter.jpg"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("\xff\xd8"+name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	p := fakePreviews{dir: dir}
	signer := playback.NewSigner([]byte("key"))
	api := New(slog.New(slog.DiscardHandler), domain.Info{}, Services{Auth: fakeAuth{}, Previews: p, PreviewFiles: p, Signer: signer})
	get := func(token, target string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		return rec
	}
	base := "/api/v1/parts/" + previewedPart.String()

	rec := get(goodToken, base+"/trickplay")
	var got store.Trickplay
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil || rec.Code != http.StatusOK || got.Sheets != 2 || got.Height != 180 {
		t.Fatalf("geometry: %d %+v %v", rec.Code, got, err)
	}
	rec = get(goodToken, base+"/trickplay/1")
	if rec.Code != http.StatusOK || rec.Body.String() != "\xff\xd8sheet1.jpg" || rec.Header().Get("Content-Type") != "image/jpeg" ||
		rec.Header().Get("Cache-Control") != "private, max-age=31536000, immutable" {
		t.Errorf("the second sheet: %d %v %q", rec.Code, rec.Header(), rec.Body)
	}
	signed := signer.Sign(base+"/chapters/0/image", time.Now().Add(time.Hour))
	if rec := get("", signed); rec.Code != http.StatusOK || rec.Body.String() != "\xff\xd8chapter.jpg" {
		t.Errorf("the first chapter's image at its signed address, with no token: %d %q", rec.Code, rec.Body)
	}
	for _, tc := range []struct {
		token, target string
		want          int
	}{
		{"", base + "/chapters/0/image", http.StatusUnauthorized},
		{goodToken, base + "/chapters/0/image", http.StatusUnauthorized},
		{"", signer.Sign(base+"/chapters/1/image", time.Now().Add(time.Hour)), http.StatusNotFound},
		{goodToken, base + "/trickplay/2", http.StatusNotFound},
		{goodToken, base + "/trickplay/-1", http.StatusNotFound},
		{goodToken, "/api/v1/parts/" + uuid.NewV7().String() + "/trickplay", http.StatusNotFound},
		{memberToken, base + "/trickplay", http.StatusNotFound},
		{memberToken, base + "/trickplay/0", http.StatusNotFound},
		{"", base + "/trickplay/0", http.StatusUnauthorized},
	} {
		if rec := get(tc.token, tc.target); rec.Code != tc.want {
			t.Errorf("%s: %d, want %d", tc.target, rec.Code, tc.want)
		}
	}
}
