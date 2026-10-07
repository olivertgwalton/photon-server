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

	"github.com/olivertgwalton/photon-server/internal/blob"
	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
)

var (
	localPoster    = uuid.MustParse("0199b3c0-0000-7000-8000-0000000000a1")
	providerPoster = uuid.MustParse("0199b3c0-0000-7000-8000-0000000000a2")
)

// fakePictures keeps a local poster under its root, and a provider's poster already cached there.
type fakePictures struct{ root string }

func (f fakePictures) Picture(_ context.Context, id uuid.UUID) (domain.Picture, error) {
	switch id {
	case localPoster:
		return domain.Picture{Root: f.root, Path: "Heat (1995)/poster.jpg"}, nil
	case providerPoster:
		return domain.Picture{URL: "https://image.tmdb.org/t/p/original/heat.jpg"}, nil
	}
	return domain.Picture{}, store.ErrNotFound
}

// Open answers a copy only of the provider's picture, unnamed as a copy is, and the local one as
// it is.
func (f fakePictures) Open(_ context.Context, id uuid.UUID, p domain.Picture, width, height int) (blob.Object, string, error) {
	switch {
	case id == providerPoster && (width > 0 || height > 0):
		o, err := openObject(filepath.Join(f.root, "small"))
		return o, "", err
	case p.URL != "":
		o, err := openObject(filepath.Join(f.root, "cached"))
		return o, "heat.jpg", err
	}
	o, err := openObject(filepath.Join(p.Root, p.Path))
	return o, "poster.jpg", err
}

func (fakePictures) Keep(context.Context, uuid.UUID, io.Reader) error {
	return errors.New("not kept here")
}

// Kept answers a theme tune fetched from ThemerrDB's link, kept as "tune".
func (f fakePictures) Kept(_ context.Context, id uuid.UUID) (blob.Object, error) {
	if id == fetchedTheme {
		return openObject(filepath.Join(f.root, "tune"))
	}
	return blob.Object{}, os.ErrNotExist
}

// openObject opens the file at path as an object.
func openObject(path string) (blob.Object, error) {
	f, err := os.Open(path)
	if err != nil {
		return blob.Object{}, err
	}
	return blob.OfFile(f)
}

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
	if rec := get(providerPoster, "?height=480"); rec.Body.String() != "\xff\xd8\xff\xe0small" {
		t.Errorf("a picture asked for by height: %q, want the copy", rec.Body.String())
	}
	if rec := get(localPoster, "?height=0"); rec.Code != http.StatusBadRequest {
		t.Errorf("height=0: %d, want 400", rec.Code)
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
