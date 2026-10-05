package artwork

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"uuid"
)

func TestAPictureIsFetchedOnce(t *testing.T) {
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
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("jpeg bytes"))
	}))
	t.Cleanup(srv.Close)
	c, err := Open(t.TempDir())
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
			if b, _ := io.ReadAll(f); string(b) != "jpeg bytes" {
				t.Errorf("read %q", b)
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

	if _, err := c.File(t.Context(), uuid.NewV7(), srv.URL+"/page.html"); err == nil {
		t.Error("a page that is not an image was kept as a picture")
	}
}
