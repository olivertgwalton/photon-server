package media

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	gocmp "github.com/google/go-cmp/cmp"
)

// served serves the keyframes fixtures over HTTP, in byte ranges where ranges is true, counting
// the requests made of it.
func served(t *testing.T, ranges bool) (*url.URL, *atomic.Int64) {
	t.Helper()
	var asked atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked.Add(1)
		if !ranges {
			r.Header.Del("Range")
		}
		http.ServeFile(w, r, filepath.Join("testdata", "keyframes", filepath.Base(r.URL.Path)))
	}))
	t.Cleanup(srv.Close)
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	return u, &asked
}

func TestAStrmsKeyframesAreReadFromItsMediasIndexInAFewRanges(t *testing.T) {
	u, asked := served(t, true)
	for name, want := range indexed {
		asked.Store(0)
		got, err := IndexedKeyframes(t.Context(), Input{URL: u.JoinPath(name)})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if diff := gocmp.Diff(want, got); diff != "" {
			t.Errorf("%s: keyframes (-want +got):\n%s", name, diff)
		}
		if n := asked.Load(); n > 4 {
			t.Errorf("%s: asked %d ranges, want the index read a block at a time", name, n)
		}
	}
	for _, name := range unindexed {
		if _, err := IndexedKeyframes(t.Context(), Input{URL: u.JoinPath(name)}); !errors.Is(err, ErrNoIndex) {
			t.Errorf("%s: %v, want no index", name, err)
		}
	}
}

func TestAStrmsMediaReadsAsTheFileItIs(t *testing.T) {
	u, _ := served(t, true)
	// Past the first block, so reads cross from one to the next.
	want, err := os.ReadFile(filepath.Join("testdata", "keyframes", "transport.ts"))
	if err != nil {
		t.Fatal(err)
	}
	r, err := openRemote(t.Context(), u.JoinPath("transport.ts"))
	if err != nil {
		t.Fatal(err)
	}
	if r.Size() != int64(len(want)) {
		t.Fatalf("size %d, want %d", r.Size(), len(want))
	}
	for _, read := range []struct{ off, n int64 }{{0, 16}, {rangeBlock - 8, 16}, {rangeBlock + 100, 300}, {0, int64(len(want))}} {
		b := make([]byte, read.n)
		if _, err := r.ReadAt(b, read.off); err != nil && !errors.Is(err, io.EOF) {
			t.Fatal(err)
		}
		if !bytes.Equal(b, want[read.off:read.off+read.n]) {
			t.Errorf("%d bytes at %d are not the file's", read.n, read.off)
		}
	}
}

func TestAStrmWhoseServerSendsNoRangesHasNoIndex(t *testing.T) {
	u, _ := served(t, false)
	if _, err := IndexedKeyframes(t.Context(), Input{URL: u.JoinPath("cues-front.mkv")}); !errors.Is(err, ErrNoIndex) {
		t.Errorf("%v, want no index: an index is read by ranges", err)
	}
}
