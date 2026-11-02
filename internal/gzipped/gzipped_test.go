package gzipped

import (
	"compress/gzip"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func reply(t *testing.T, encoding string, h http.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	if encoding != "" {
		r.Header.Set("Accept-Encoding", encoding)
	}
	w := httptest.NewRecorder()
	JSON(h, slog.New(slog.DiscardHandler)).ServeHTTP(w, r)
	return w
}

func written(t *testing.T, contentType string, status int, body string) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(status)
		// Written a piece at a time, as a long list is.
		for piece := range strings.SplitAfterSeq(body, ",") {
			if _, err := io.WriteString(w, piece); err != nil {
				t.Error(err)
			}
		}
	}
}

func TestALongJSONReplyIsGzippedAsItIsWritten(t *testing.T) {
	body := `{"Items":[` + strings.Repeat(`{"Name":"Heat","Type":"Movie"},`, 500) + `{}]}`
	w := reply(t, "gzip, deflate, br", written(t, "application/json; charset=utf-8", http.StatusOK, body))
	if w.Header().Get("Content-Encoding") != "gzip" || w.Header().Get("Vary") != "Accept-Encoding" || w.Header().Get("Content-Length") != "" {
		t.Fatalf("headers %v: want gzip, varying by Accept-Encoding, of no stated length", w.Header())
	}
	r, err := gzip.NewReader(w.Body)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != body {
		t.Errorf("gzipped reply decodes to %.80q, want the JSON written", got)
	}
}

func TestWhatIsNotWorthGzippingIsSentAsItIs(t *testing.T) {
	long := strings.Repeat(`{"Name":"Heat"},`, 200)
	for _, c := range []struct {
		name, encoding string
		h              http.HandlerFunc
	}{
		{"a short reply", "gzip", written(t, "application/json", http.StatusOK, `{"Id":"1"}`)},
		{"a video", "gzip", written(t, "video/mp4", http.StatusOK, long)},
		{"an error", "gzip", written(t, "application/json", http.StatusInternalServerError, long)},
		{"to a client that takes no gzip", "", written(t, "application/json", http.StatusOK, long)},
		{"to a client that refuses gzip", "br, gzip;q=0", written(t, "application/json", http.StatusOK, long)},
	} {
		w := reply(t, c.encoding, c.h)
		want := httptest.NewRecorder()
		c.h(want, httptest.NewRequest(http.MethodGet, "/", nil))
		if w.Header().Get("Content-Encoding") != "" || w.Code != want.Code || w.Body.String() != want.Body.String() {
			t.Errorf("%s: %d, Content-Encoding %q, %d bytes; want it as it is", c.name, w.Code, w.Header().Get("Content-Encoding"), w.Body.Len())
		}
	}
	if w := reply(t, "gzip", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }); w.Code != http.StatusNoContent || w.Body.Len() != 0 {
		t.Errorf("no content: %d, %d bytes", w.Code, w.Body.Len())
	}
}

// A reply written whole is gzipped where it is long enough and the client takes gzip, and sent as
// it is otherwise, each saying it varies by Accept-Encoding, once.
func TestAWholeReplyIsGzippedWhereItIsWorthIt(t *testing.T) {
	long := strings.Repeat("#EXTINF:6.000000,\n7.m4s\n", 100)
	for _, c := range []struct {
		name, encoding, body, encoded string
		zipped                        bool
	}{
		{"long, for a client of gzip", "gzip", long, "", true},
		{"long, for a client of none", "", long, "", false},
		{"short", "gzip", "#EXTM3U\n", "", false},
		{"already encoded", "gzip", long, "br", false},
	} {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		if c.encoding != "" {
			r.Header.Set("Accept-Encoding", c.encoding)
		}
		w := httptest.NewRecorder()
		w.Header().Add("Vary", "Accept-Encoding")
		if c.encoded != "" {
			w.Header().Set("Content-Encoding", c.encoded)
		}
		if err := Reply(w, r, http.StatusOK, []byte(c.body)); err != nil {
			t.Fatal(err)
		}
		if vary := w.Header().Values("Vary"); len(vary) != 1 || vary[0] != "Accept-Encoding" {
			t.Errorf("%s: Vary %v, want Accept-Encoding once", c.name, vary)
		}
		got := w.Body.String()
		if c.zipped {
			if w.Header().Get("Content-Encoding") != "gzip" {
				t.Fatalf("%s: not gzipped", c.name)
			}
			zr, err := gzip.NewReader(w.Body)
			if err != nil {
				t.Fatal(err)
			}
			b, err := io.ReadAll(zr)
			if err != nil {
				t.Fatal(err)
			}
			got = string(b)
		} else if w.Header().Get("Content-Encoding") == "gzip" {
			t.Errorf("%s: gzipped", c.name)
		}
		if got != c.body {
			t.Errorf("%s: %.40q, want %.40q", c.name, got, c.body)
		}
	}
}
