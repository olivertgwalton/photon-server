package httpapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/artwork"
	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
)

var (
	localPoster    = uuid.MustParse("0199b3c0-0000-7000-8000-0000000000a1")
	providerPoster = uuid.MustParse("0199b3c0-0000-7000-8000-0000000000a2")
)

// fakePictures keeps a local poster under its root, and a provider's poster already cached there.
type fakePictures struct{ root string }

func (f fakePictures) Picture(_ context.Context, id uuid.UUID) (store.Picture, error) {
	switch id {
	case localPoster:
		return store.Picture{Root: f.root, Path: "Heat (1995)/poster.jpg"}, nil
	case providerPoster:
		return store.Picture{URL: "https://image.tmdb.org/t/p/original/heat.jpg"}, nil
	}
	return store.Picture{}, store.ErrNotFound
}

func (f fakePictures) File(context.Context, uuid.UUID, string) (*os.File, error) {
	return os.Open(filepath.Join(f.root, "cached"))
}

// Resized answers a copy only of the provider's picture, and the local one as it is.
func (f fakePictures) Resized(_ context.Context, key string, _ int, _ func(context.Context) (*os.File, error)) (*os.File, error) {
	if key == providerPoster.String() {
		return os.Open(filepath.Join(f.root, "small"))
	}
	return nil, artwork.ErrNotResizable
}

// Sound answers the theme host's tune, kept as "tune".
func (f fakePictures) Sound(context.Context, uuid.UUID, string) (*os.File, error) {
	return os.Open(filepath.Join(f.root, "tune"))
}

func (fakePictures) Keep(uuid.UUID, io.Reader) error { return errors.New("not kept here") }

func (fakePictures) Kept(uuid.UUID) (*os.File, error) { return nil, os.ErrNotExist }

func TestArtwork(t *testing.T) {
	root := t.TempDir()
	for name, body := range map[string]string{"Heat (1995)/poster.jpg": "local jpeg", "cached": "provider jpeg", "small": "\xff\xd8\xff\xe0small"} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	pics := fakePictures{root: root}
	api := New(slog.New(slog.DiscardHandler), domain.Info{}, Services{Auth: fakeAuth{}, Pictures: pics, Artwork: pics})
	get := func(id uuid.UUID, query ...string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/artwork/"+id.String()+strings.Join(query, ""), nil))
		return rec
	}
	for id, want := range map[uuid.UUID]string{localPoster: "local jpeg", providerPoster: "provider jpeg"} {
		rec := get(id)
		if rec.Code != http.StatusOK || rec.Body.String() != want {
			t.Errorf("%v: %d %q, want 200 %q without a token", id, rec.Code, rec.Body.String(), want)
		}
		if rec.Header().Get("Content-Type") != "image/jpeg" || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("%v: headers %v, want image/jpeg, nosniff", id, rec.Header())
		}
	}
	if rec := get(providerPoster, "?width=320"); rec.Body.String() != "\xff\xd8\xff\xe0small" || rec.Header().Get("Content-Type") != "image/jpeg" {
		t.Errorf("a resized picture: %q as %q, want the copy, typed by its content", rec.Body.String(), rec.Header().Get("Content-Type"))
	}
	if rec := get(localPoster, "?width=320"); rec.Body.String() != "local jpeg" {
		t.Errorf("a picture that cannot be resized: %q, want it as it is", rec.Body.String())
	}
	if rec := get(localPoster, "?width=wide"); rec.Code != http.StatusBadRequest {
		t.Errorf("width=wide: %d, want 400", rec.Code)
	}
	if rec := get(uuid.NewV7()); rec.Code != http.StatusNotFound {
		t.Errorf("an unknown picture: %d, want 404", rec.Code)
	}
}
