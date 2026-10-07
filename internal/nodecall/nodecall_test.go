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
	other, _ := NewKey([]byte("another cluster's key"))
	var got []byte
	h := key.Verify(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusNoContent)
	}))
	body := []byte(`{"item":"heat"}`)
	signed := func(k Key, path string, b []byte) *http.Request {
		r := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
		k.Sign(r, b)
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
