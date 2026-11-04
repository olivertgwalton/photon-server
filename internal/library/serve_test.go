package library

import (
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAStrmIsServedAsTheMediaItNames(t *testing.T) {
	body := strings.Repeat("0123456789", 1000)
	var asked http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = r.Header.Clone()
		http.SetCookie(w, &http.Cookie{Name: "session", Value: "debrid"})
		http.ServeContent(w, r, "heat.mkv", time.Unix(0, 0), strings.NewReader(body))
	}))
	defer srv.Close()
	dir := t.TempDir()
	write(t, dir, map[string]string{"Heat (1995).strm": srv.URL + "/heat.mkv\n"})

	serve := func(limit int64, header http.Header) (*httptest.ResponseRecorder, string) {
		t.Helper()
		in, err := OpenMedia(dir, "Heat (1995).strm")
		if err != nil {
			t.Fatal(err)
		}
		defer in.Close()
		r := httptest.NewRequest(http.MethodGet, "/stream", nil)
		r.Header = header
		w := httptest.NewRecorder()
		if err := Serve(w, r, in, limit); err != nil {
			t.Fatal(err)
		}
		return w, w.Body.String()
	}

	w, got := serve(math.MaxInt64, http.Header{"Range": {"bytes=10-19"}, "Authorization": {"Bearer player"}})
	if w.Code != http.StatusPartialContent || got != body[10:20] || w.Header().Get("Content-Range") != "bytes 10-19/10000" {
		t.Errorf("a range answered %d %q (%s), want bytes 10-19 of the media", w.Code, got, w.Header().Get("Content-Range"))
	}
	if asked.Get("Authorization") != "" {
		t.Error("the player's credentials were sent on to the media's server")
	}
	if w.Header().Get("Set-Cookie") != "" {
		t.Error("the media's server's cookie was passed on to the player")
	}

	if _, got = serve(16, http.Header{"Range": {"bytes=500-"}}); got != body[:16] {
		t.Errorf("a sample of 16 bytes answered %q, want the media's first 16", got)
	}
}

// A server that answers more of the media than was asked has only the sample's limit passed on.
func TestASampleOfAStrmIsHeldToItsLimit(t *testing.T) {
	body := strings.Repeat("0123456789", 100)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Range", "bytes 0-999/1000")
		w.Header().Set("Content-Length", "1000")
		w.WriteHeader(http.StatusPartialContent)
		if _, err := w.Write([]byte(body)); err != nil {
			t.Error(err)
		}
	}))
	defer srv.Close()
	dir := t.TempDir()
	write(t, dir, map[string]string{"Heat (1995).strm": srv.URL + "/heat.mkv\n"})
	in, err := OpenMedia(dir, "Heat (1995).strm")
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	w := httptest.NewRecorder()
	if err := Serve(w, httptest.NewRequest(http.MethodGet, "/sample", nil), in, 16); err != nil {
		t.Fatal(err)
	}
	if got := w.Body.String(); got != body[:16] || w.Header().Get("Content-Length") != "" {
		t.Errorf("a sample of 16 bytes sent %d bytes with Content-Length %q, want the first 16 and no length", len(got), w.Header().Get("Content-Length"))
	}
}

// An EPUB is sent as a download of its own type, never as a page a browser would run its script
// in, and in the ranges a reader asks for.
func TestAnEbookIsServedAsADownload(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, map[string]string{"Earthsea.epub": "PK<html><script>alert(1)</script></html>"})
	in, err := OpenMedia(dir, "Earthsea.epub")
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Range", "bytes=0-1")
	w := httptest.NewRecorder()
	if err := Serve(w, r, in, math.MaxInt64); err != nil {
		t.Fatal(err)
	}
	h := w.Header()
	if w.Code != http.StatusPartialContent || w.Body.String() != "PK" {
		t.Errorf("%d %q, want its first two bytes", w.Code, w.Body)
	}
	if h.Get("Content-Type") != "application/epub+zip" || h.Get("Content-Disposition") != `attachment; filename=Earthsea.epub` ||
		h.Get("X-Content-Type-Options") != "nosniff" || !strings.Contains(h.Get("Content-Security-Policy"), "sandbox") {
		t.Errorf("headers %v, want a sandboxed download of an EPUB", h)
	}
}

func TestATrackIsServedAsTheSoundItIs(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, map[string]string{"Wake Me Up.flac": "fLaC and its frames", "Earthsea.m4b": "\x00\x00\x00\x18ftypM4B \x00\x00\x02\x00"})
	for name, want := range map[string]string{"Wake Me Up.flac": "audio/flac", "Earthsea.m4b": "audio/mp4"} {
		in, err := OpenMedia(dir, name)
		if err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		if err := Serve(w, httptest.NewRequest(http.MethodGet, "/", nil), in, math.MaxInt64); err != nil {
			t.Fatal(err)
		}
		in.Close()
		if got := w.Header().Get("Content-Type"); got != want {
			t.Errorf("%s served as %q, want %q", name, got, want)
		}
	}
}
