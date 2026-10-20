package nodecall

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// A node honours what another node of its cluster signed, unchanged and in time, and nothing a
// client could send: no signature, one of another cluster's, a body or path changed after
// signing, or a signature past its time.
func TestOnlyANodeOfTheClusterIsHonoured(t *testing.T) {
	key, err := NewKey([]byte("cluster signing key"))
	if err != nil {
		t.Fatal(err)
	}
	other, err := NewKey([]byte("another cluster's key"))
	if err != nil {
		t.Fatal(err)
	}
	var got []byte
	h := key.Verify(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		if got, err = io.ReadAll(r.Body); err != nil {
			t.Error(err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	body := []byte(`{"item":"heat"}`)
	signed := func(k Key, path string, b []byte) *http.Request {
		r := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
		k.signUntil(r, b, time.Now().Add(within))
		return r
	}
	answer := func(r *http.Request) int {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		return rec.Code
	}
	const path = "/api/v1/internal/playbacks/p/remux"
	if code := answer(signed(key, path, body)); code != http.StatusNoContent || !bytes.Equal(got, body) {
		t.Errorf("a node's request: %d, body %q; want it honoured, as sent", code, got)
	}
	changed := signed(key, path, body)
	changed.Body = io.NopCloser(strings.NewReader(`{"item":"alien"}`))
	moved := signed(key, path, body)
	moved.URL.Path = "/api/v1/internal/playbacks/q/remux"
	late := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	key.signUntil(late, body, time.Now().Add(-time.Second))
	for name, r := range map[string]*http.Request{
		"unsigned":            httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body)),
		"another cluster's":   signed(other, path, body),
		"a body changed":      changed,
		"to another playback": moved,
		"past its time":       late,
	} {
		if code := answer(r); code != http.StatusUnauthorized {
			t.Errorf("%s: %d, want 401", name, code)
		}
	}
}

// A node that answers a call with a redirect is not followed there, so the signature never
// reaches the server it points to.
func TestACallIsNotRedirected(t *testing.T) {
	key, err := NewKey([]byte("cluster signing key"))
	if err != nil {
		t.Fatal(err)
	}
	var signature string
	elsewhere := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		signature = r.Header.Get(header)
	}))
	defer elsewhere.Close()
	node := httptest.NewServer(http.RedirectHandler(elsewhere.URL+"/api/v1/internal/playbacks/p/delivery", http.StatusTemporaryRedirect))
	defer node.Close()
	r, err := http.NewRequestWithContext(t.Context(), http.MethodPost, node.URL+"/api/v1/internal/playbacks/p/delivery", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := key.Do(r, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusTemporaryRedirect || signature != "" {
		t.Errorf("answered %s, and the redirect's target was sent %q; want the redirect, unfollowed", resp.Status, signature)
	}
}
