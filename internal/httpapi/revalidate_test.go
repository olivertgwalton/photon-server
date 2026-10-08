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
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/api/v1/admin/events", nil)
	if err != nil {
		t.Fatal(err)
	}
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

func revalidated(target, encoding, tags string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.Header.Set("Authorization", "Bearer "+goodToken)
	req.Header.Set("Accept-Encoding", encoding)
	req.Header.Set("If-None-Match", tags)
	rec := httptest.NewRecorder()
	newAPI(nil).ServeHTTP(rec, req)
	return rec
}

func TestAnAnswerTheClientHoldsIsNotSentAgain(t *testing.T) {
	// The home is big enough to gzip; a title page is not.
	for _, target := range []string{"/api/v1/home?limit=50", "/api/v1/titles/" + films.String()} {
		plain := asked(target, "")
		tag := plain.Header().Get("ETag")
		if !strings.HasPrefix(tag, `W/"`) || plain.Header().Get("Cache-Control") != "private, no-cache" {
			t.Fatalf("%s: ETag %q, Cache-Control %q: want a weak tag the client revalidates", target, tag, plain.Header().Get("Cache-Control"))
		}
		if zipped := asked(target, "gzip"); zipped.Header().Get("ETag") != tag {
			t.Errorf("%s: ETag %q gzipped, %q plain: want the same", target, zipped.Header().Get("ETag"), tag)
		}
		for _, encoding := range []string{"", "gzip"} {
			for _, tags := range []string{tag, strings.TrimPrefix(tag, "W/"), `"other", ` + tag, "*"} {
				rec := revalidated(target, encoding, tags)
				if rec.Code != http.StatusNotModified || rec.Body.Len() != 0 || rec.Header().Get("Content-Encoding") != "" ||
					rec.Header().Get("ETag") != tag || rec.Header().Get("Vary") != "Accept-Encoding" {
					t.Errorf("%s, Accept-Encoding %q, If-None-Match %s: status %d, %d bytes, headers %v: want an empty 304 with the tag",
						target, encoding, tags, rec.Code, rec.Body.Len(), rec.Header())
				}
			}
			if rec := revalidated(target, encoding, `W/"other"`); rec.Code != http.StatusOK || rec.Body.Len() == 0 {
				t.Errorf("%s, Accept-Encoding %q, another tag: status %d, want the answer", target, encoding, rec.Code)
			}
		}
	}
}

func TestAChangeGivesANewTag(t *testing.T) {
	api := New(slog.New(slog.DiscardHandler), domain.Info{}, Services{Auth: fakeAuth{}, Preferences: &fakePreferences{}})
	call := func(method, body, tags string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/api/v1/profile/preferences", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+goodToken)
		req.Header.Set("If-None-Match", tags)
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		return rec
	}
	before := call(http.MethodGet, "", "").Header().Get("ETag")
	if patch := call(http.MethodPatch, `{"subtitle_mode":"always"}`, before); patch.Code != http.StatusOK || patch.Header().Get("ETag") != "" {
		t.Fatalf("PATCH: status %d, ETag %q: want the answer, untagged", patch.Code, patch.Header().Get("ETag"))
	}
	after := call(http.MethodGet, "", before)
	if after.Code != http.StatusOK || after.Header().Get("ETag") == before {
		t.Errorf("GET after a change, with the old tag: status %d, ETag %q: want the new answer under a new tag", after.Code, after.Header().Get("ETag"))
	}
}

func TestAWriteIsNeverTagged(t *testing.T) {
	rec := serve(t, http.MethodPost, "/api/v1/auth/login", "", `{"method":"password","name":"Oliver","password":"correct horse","device":"Living room","client":"Photon"}`)
	if rec.Code != http.StatusOK || rec.Header().Get("ETag") != "" {
		t.Errorf("login: status %d, ETag %q: want no tag", rec.Code, rec.Header().Get("ETag"))
	}
}
