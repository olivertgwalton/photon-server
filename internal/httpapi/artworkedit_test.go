package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	_ "image/jpeg"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/artwork"
	"github.com/olivertgwalton/photon-server/internal/blob"
	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// Picture answers a picture an admin uploaded as kept by the server, while it is the film's.
func (f *fakeEditing) Picture(_ context.Context, id uuid.UUID) (domain.Picture, error) {
	for _, p := range f.chosen {
		if p == id && p != poster {
			return domain.Picture{Kept: true}, nil
		}
	}
	return domain.Picture{}, store.ErrNotFound
}

func TestAnAdminUploadsATitlesPicture(t *testing.T) {
	blobs, err := blob.OpenDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = blobs.Close() })
	e := &fakeEditing{}
	api := New(slog.New(slog.DiscardHandler), domain.Info{}, Services{
		Auth: fakeAuth{}, Editing: e, Pictures: e, Artwork: artwork.New(blobs, nil), Events: &fakeEvents{},
	})
	do := func(token, method, target string, body []byte) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, bytes.NewReader(body))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		return rec
	}
	base := "/api/v1/admin/titles/" + films.String() + "/artwork/"

	rec := do(goodToken, http.MethodPost, base+"poster", pngOf(300, 450))
	var up artworkCandidateJSON
	if rec.Code != http.StatusCreated || json.Unmarshal(rec.Body.Bytes(), &up) != nil ||
		up.Source != domain.SourceUpload || !up.Chosen || up.Width != 300 || up.Height != 450 {
		t.Fatalf("POST poster = %d %s", rec.Code, rec.Body)
	}
	if e.uploaded.Kind != domain.ArtworkPoster || e.uploaded.Blurhash == "" {
		t.Errorf("kept as %+v; want the poster, with its BlurHash", e.uploaded)
	}
	picture := "/api/v1/artwork/" + up.ID.String()
	if got := do("", http.MethodGet, picture, nil); got.Code != http.StatusOK || !bytes.Equal(got.Body.Bytes(), pngOf(300, 450)) {
		t.Errorf("the upload served = %d, %d bytes", got.Code, got.Body.Len())
	}
	sized := do("", http.MethodGet, picture+"?width=100", nil)
	if cfg, _, err := image.DecodeConfig(sized.Body); sized.Code != http.StatusOK || err != nil || cfg.Width >= 300 {
		t.Errorf("the upload served smaller = %d, %+v, %v", sized.Code, cfg, err)
	}

	for _, tc := range []struct {
		name, token, target string
		body                []byte
		want                int
		has                 string
	}{
		{"a member", memberToken, base + "poster", pngOf(8, 8), http.StatusForbidden, ""},
		{"text", goodToken, base + "poster", []byte("hello, this is not a picture at all"), http.StatusBadRequest, "not a picture"},
		{"an SVG", goodToken, base + "logo", []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`), http.StatusBadRequest, "not a picture"},
		{"over 32 MiB", goodToken, base + "backdrop", make([]byte, 32<<20+1), http.StatusBadRequest, "over 32 MiB"},
		{"no such kind", goodToken, base + "disc", pngOf(8, 8), http.StatusNotFound, ""},
		{"no such title", goodToken, "/api/v1/admin/titles/" + uuid.NewV7().String() + "/artwork/poster", pngOf(8, 8), http.StatusNotFound, ""},
	} {
		if rec := do(tc.token, http.MethodPost, tc.target, tc.body); rec.Code != tc.want || !strings.Contains(rec.Body.String(), tc.has) {
			t.Errorf("%s: %d %s, want %d with %q", tc.name, rec.Code, rec.Body, tc.want, tc.has)
		}
	}

	if rec := do(goodToken, http.MethodDelete, base+"poster", nil); rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE poster = %d %s", rec.Code, rec.Body)
	}
	if got := do("", http.MethodGet, picture, nil); got.Code != http.StatusNotFound {
		t.Errorf("the upload given back is still served: %d", got.Code)
	}
}
