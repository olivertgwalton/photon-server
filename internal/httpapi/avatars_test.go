package httpapi

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"image"
	"image/png"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/artwork"
	"github.com/olivertgwalton/photon-server/internal/blob"
	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// fakeAvatars keeps each profile's picture, and answers a picture as kept while a profile has it.
type fakeAvatars struct {
	mu  sync.Mutex
	has map[uuid.UUID]uuid.UUID
}

func (f *fakeAvatars) SetAvatar(_ context.Context, profile, picture uuid.UUID, _ *uuid.UUID) (domain.Profile, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.has[profile] = picture
	return domain.Profile{ID: profile, Avatar: picture}, nil
}

func (f *fakeAvatars) Picture(_ context.Context, id uuid.UUID) (domain.Picture, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, p := range f.has {
		if p == id && id != (uuid.UUID{}) {
			return domain.Picture{Kept: true}, nil
		}
	}
	return domain.Picture{}, store.ErrNotFound
}

func pngOf(w, h int) []byte {
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewGray(image.Rect(0, 0, w, h))); err != nil {
		panic(err)
	}
	return b.Bytes()
}

// claiming is a small PNG whose header says it is w by h, as a decompression bomb's does.
func claiming(w, h uint32) []byte {
	b := pngOf(1, 1)
	ihdr := b[12 : 12+4+13]
	binary.BigEndian.PutUint32(ihdr[4:], w)
	binary.BigEndian.PutUint32(ihdr[8:], h)
	binary.BigEndian.PutUint32(b[12+4+13:], crc32.ChecksumIEEE(ihdr))
	return b
}

func TestAProfileIsGivenAPicture(t *testing.T) {
	blobs, err := blob.OpenDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = blobs.Close() })
	cache := artwork.New(blobs, nil)
	avatars := &fakeAvatars{has: map[uuid.UUID]uuid.UUID{}}
	api := New(slog.New(slog.DiscardHandler), domain.Info{}, Services{Auth: fakeAuth{}, Avatars: avatars, Pictures: avatars, Artwork: cache})
	do := func(token, method, target string, body []byte) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, bytes.NewReader(body))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		return rec
	}

	rec := do(goodToken, http.MethodPost, "/api/v1/me/avatar", pngOf(64, 64))
	var me profileJSON
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &me) != nil || me.Avatar == (uuid.UUID{}) {
		t.Fatalf("POST avatar = %d %s", rec.Code, rec.Body)
	}
	if got := do("", http.MethodGet, "/api/v1/artwork/"+me.Avatar.String(), nil); got.Code != http.StatusOK || !bytes.Equal(got.Body.Bytes(), pngOf(64, 64)) {
		t.Errorf("the picture served = %d, %d bytes", got.Code, got.Body.Len())
	}

	for name, body := range map[string][]byte{
		"text":                []byte("hello, this is not a picture at all"),
		"an SVG":              []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`),
		"a PNG said too wide": claiming(20_000, 20_000),
		"a PNG cut short":     pngOf(8, 8)[:20],
	} {
		if rec := do(memberToken, http.MethodPost, "/api/v1/me/avatar", body); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "not a picture") {
			t.Errorf("%s: %d %s, want 400", name, rec.Code, rec.Body)
		}
	}

	other := uuid.NewV7()
	if rec := do(memberToken, http.MethodPost, "/api/v1/admin/profiles/"+other.String()+"/avatar", pngOf(8, 8)); rec.Code != http.StatusForbidden {
		t.Errorf("a member setting another's picture: %d, want 403", rec.Code)
	}
	if rec := do(goodToken, http.MethodPost, "/api/v1/admin/profiles/"+other.String()+"/avatar", pngOf(8, 8)); rec.Code != http.StatusOK {
		t.Errorf("an admin setting another's picture: %d %s", rec.Code, rec.Body)
	}
	if rec := do(goodToken, http.MethodDelete, "/api/v1/me/avatar", nil); rec.Code != http.StatusNoContent {
		t.Errorf("DELETE avatar = %d", rec.Code)
	}
	if got := do("", http.MethodGet, "/api/v1/artwork/"+me.Avatar.String(), nil); got.Code != http.StatusNotFound {
		t.Errorf("the picture taken away is still served: %d", got.Code)
	}
}
