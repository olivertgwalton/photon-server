package playback

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// bigFile is a file of size bytes, sparse so it costs no disk.
func bigFile(t *testing.T, size int64) *os.File {
	t.Helper()
	f, err := os.Create(filepath.Join(t.TempDir(), "film.mkv"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	if err := f.Truncate(size); err != nil {
		t.Fatal(err)
	}
	return f
}

// A file played as it is counts as sent while it is still being sent, not only once it ends.
func TestADirectPlayIsCountedAsItIsSent(t *testing.T) {
	const size = 256 << 20
	f := bigFile(t, size)
	sent := NewSent()
	srv := httptest.NewServer(sent.Counting(DeliveryFile, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "film.mkv", time.Time{}, f)
	})))
	defer srv.Close()
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	// The player has read a little and stalls, as one paused does, the rest unsent.
	if _, err := io.CopyN(io.Discard, resp.Body, 3*sentChunk); err != nil {
		t.Fatal(err)
	}
	// The third chunk's bytes go out only after the first two are sent and counted.
	if got := testutil.ToFloat64(sent.bytes.WithLabelValues(string(DeliveryFile))); got < 2*sentChunk || got >= size {
		t.Errorf("%v bytes counted with %d read of %d, want at least the first two chunks and not the whole", got, 3*sentChunk, size)
	}
}

// readerFroms records what a response is handed to send.
type readerFroms struct {
	http.ResponseWriter
	got []io.Reader
}

func (r *readerFroms) ReadFrom(src io.Reader) (int64, error) {
	r.got = append(r.got, src)
	return io.Copy(io.Discard, src)
}

// Each chunk of a file reaches the connection as net's sendfile takes it: the file itself in one
// LimitedReader.
func TestAFileIsHandedOnForSendfile(t *testing.T) {
	const size = 2*sentChunk + 100
	f := bigFile(t, size)
	under := &readerFroms{ResponseWriter: httptest.NewRecorder()}
	sent := NewSent()
	c := &counting{ResponseWriter: under, sent: sent.bytes.WithLabelValues(string(DeliveryFile))}
	src := io.LimitReader(f, size).(*io.LimitedReader)
	n, err := c.ReadFrom(src)
	if err != nil || n != size || src.N != 0 {
		t.Fatalf("sent %d, %v, %d left; want all %d", n, err, src.N, size)
	}
	if len(under.got) != 3 {
		t.Fatalf("handed on in %d chunks, want 3", len(under.got))
	}
	for i, r := range under.got {
		if lr, ok := r.(*io.LimitedReader); !ok || lr.R != f {
			t.Errorf("chunk %d is a %T, want a LimitedReader around the file", i, r)
		}
	}
	if got := testutil.ToFloat64(c.sent); got != size {
		t.Errorf("counted %v, want %d", got, size)
	}
}
