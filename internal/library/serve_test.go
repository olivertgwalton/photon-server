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
		f, err := Open(dir, "Heat (1995).strm")
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		r := httptest.NewRequest(http.MethodGet, "/stream", nil)
		r.Header = header
		w := httptest.NewRecorder()
		if err := Serve(w, r, f, "Heat (1995).strm", limit); err != nil {
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
