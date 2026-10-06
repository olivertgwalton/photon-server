package artwork

import (
	"bytes"
	"context"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"uuid"
)

func TestAPictureIsFetchedOnceAndHashed(t *testing.T) {
	var poster bytes.Buffer
	if err := png.Encode(&poster, gradient(20, 30)); err != nil {
		t.Fatal(err)
	}
	var fetches atomic.Int32
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetches.Add(1)
		<-release
		if r.URL.Path == "/page.html" {
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<html>"))
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(poster.Bytes())
	}))
	t.Cleanup(srv.Close)
	var mu sync.Mutex
	hashed := map[uuid.UUID]string{}
	c, err := Open(t.TempDir(), func(_ context.Context, id uuid.UUID, hash string) error {
		mu.Lock()
		defer mu.Unlock()
		if _, ok := hashed[id]; ok {
			t.Errorf("%s hashed twice", id)
		}
		hashed[id] = hash
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })

	id := uuid.NewV7()
	var wg sync.WaitGroup
	for range 5 {
		wg.Go(func() {
			f, err := c.File(t.Context(), id, srv.URL+"/poster.jpg")
			if err != nil {
				t.Error(err)
				return
			}
			defer f.Close()
			if b, _ := io.ReadAll(f); !bytes.Equal(b, poster.Bytes()) {
				t.Errorf("read %d bytes, want the poster's %d", len(b), poster.Len())
			}
		})
	}
	close(release)
	wg.Wait()
	if _, err := c.File(t.Context(), id, srv.URL+"/poster.jpg"); err != nil {
		t.Fatal(err)
	}
	if n := fetches.Load(); n != 1 {
		t.Errorf("fetched %d times for six asks, want once", n)
	}
	if hashed[id] == "" {
		t.Error("the poster fetched has no BlurHash")
	}

	if _, err := c.File(t.Context(), uuid.NewV7(), srv.URL+"/page.html"); err == nil {
		t.Error("a page that is not an image was kept as a picture")
	}
}

func TestASweepClearsReplacedPictures(t *testing.T) {
	dir := t.TempDir()
	c, err := Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	kept, gone := uuid.NewV7(), uuid.NewV7()
	old := time.Now().Add(-2 * time.Hour)
	for _, f := range []struct {
		name string
		at   time.Time
	}{
		{kept.String(), time.Now()},
		{kept.String() + "-w300", time.Now()},
		{gone.String(), time.Now()},
		{gone.String() + "-w300", time.Now()},
		{gone.String() + "-w600.as-is", time.Now()},
		{uuid.NewV7().String() + ".part", old},
		{uuid.NewV7().String() + ".part", time.Now()},
		{"README", time.Now()},
	} {
		path := filepath.Join(dir, f.name)
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, f.at, f.at); err != nil {
			t.Fatal(err)
		}
	}
	n, err := c.Sweep(t.Context(), func(_ context.Context, ids []uuid.UUID) (map[uuid.UUID]bool, error) {
		return map[uuid.UUID]bool{kept: true}, nil
	})
	if err != nil || n != 4 {
		t.Errorf("removed %d, %v; want the replaced picture's three files and the abandoned part", n, err)
	}
	left, _ := os.ReadDir(dir)
	if len(left) != 4 {
		t.Errorf("%d files left, want the kept picture's two, the part under way, and the stranger", len(left))
	}
}
