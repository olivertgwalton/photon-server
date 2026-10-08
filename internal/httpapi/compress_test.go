package httpapi

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

func asked(target, encoding string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.Header.Set("Authorization", "Bearer "+goodToken)
	req.Header.Set("Accept-Encoding", encoding)
	rec := httptest.NewRecorder()
	newAPI(nil).ServeHTTP(rec, req)
	return rec
}

func TestALargeJSONAnswerIsGzipped(t *testing.T) {
	plain := asked("/api/v1/home?limit=50", "")
	zipped := asked("/api/v1/home?limit=50", "gzip, deflate, br")
	if plain.Header().Get("Content-Encoding") != "" || zipped.Header().Get("Content-Encoding") != "gzip" ||
		zipped.Header().Get("Vary") != "Accept-Encoding" {
		t.Fatalf("Content-Encoding %q then %q, Vary %q: want gzip only when asked", plain.Header().Get("Content-Encoding"),
			zipped.Header().Get("Content-Encoding"), zipped.Header().Get("Vary"))
	}
	r, err := gzip.NewReader(zipped.Body)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plain.Body.Bytes()) || zipped.Body.Len() >= plain.Body.Len() {
		t.Errorf("gzipped %d bytes decoding to %d, plain %d: want the same JSON, smaller", zipped.Body.Len(), len(got), plain.Body.Len())
	}
}

func TestASmallJSONAnswerIsLeftAlone(t *testing.T) {
	for _, target := range []string{"/api/v1/libraries/" + films.String() + "/letters", "/api/v1/titles/" + films.String()} {
		rec := asked(target, "gzip")
		if rec.Header().Get("Content-Encoding") != "" || !strings.HasPrefix(rec.Body.String(), "{") {
			t.Errorf("%s: Content-Encoding %q, body %q: want it as it is", target, rec.Header().Get("Content-Encoding"), rec.Body)
		}
	}
}

func TestAnEventStreamIsNeverCompressed(t *testing.T) {
	t.Parallel()
	work := &fakeWork{}
	told := streamingEvents{fakeEvents: &fakeEvents{}, events: make(chan domain.Event), gone: make(chan struct{})}
	srv := httptest.NewServer(New(slog.New(slog.DiscardHandler), domain.Info{}, Services{Auth: fakeAuth{}, Tasks: work, Jobs: work, NowPlaying: work, Events: told, Profiles: listedProfiles{oliver}, Libraries: &fakeLibraries{}}))
	t.Cleanup(srv.Close)
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/api/v1/admin/events", nil)
	req.Header.Set("Authorization", "Bearer "+goodToken)
	req.Header.Set("Accept-Encoding", "gzip")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { res.Body.Close() })
	if res.Header.Get("Content-Encoding") != "" {
		t.Fatalf("Content-Encoding %q, want none", res.Header.Get("Content-Encoding"))
	}
	lines := make(chan string, 64)
	go func() {
		for s := bufio.NewScanner(res.Body); s.Scan(); {
			lines <- s.Text()
		}
		close(lines)
	}()
	sent := false
	deadline := time.After(5 * time.Second)
	for {
		select {
		case line, ok := <-lines:
			if !ok {
				t.Fatal("the stream ended")
			}
			if line == "event: "+string(domain.EventScanProgress) {
				return
			}
			if !sent && strings.HasPrefix(line, "data:") {
				sent = true
				told.events <- domain.Event{Kind: domain.EventScanProgress}
			}
		case <-deadline:
			t.Fatal("the event did not arrive")
		}
	}
}
