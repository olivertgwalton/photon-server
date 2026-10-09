package media

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	gocmp "github.com/google/go-cmp/cmp"
)

// remote serves a keyframes fixture over HTTP, in ranges, as a .strm's media is served, and
// answers it as an input whose descriptor is the .strm.
func remote(t *testing.T, name string) Input {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, filepath.Join("testdata", "keyframes", name))
	}))
	t.Cleanup(srv.Close)
	u, err := url.Parse(srv.URL + "/" + name)
	if err != nil {
		t.Fatal(err)
	}
	strm := filepath.Join(t.TempDir(), "Film.strm")
	if err := os.WriteFile(strm, []byte(u.String()+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(strm)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return Input{File: f, URL: u}
}

func TestAStrmsMediaIsReadWhereItNames(t *testing.T) {
	tools := ffprobe(t)
	in := remote(t, "cues-front.mkv")
	facts, err := tools.Probe(t.Context(), in)
	if err != nil {
		t.Fatal(err)
	}
	if facts.Container != "matroska,webm" || len(facts.Streams) == 0 {
		t.Errorf("probed %q with %d streams, want the Matroska file it names", facts.Container, len(facts.Streams))
	}
	got, err := tools.WalkKeyframes(t.Context(), in)
	if err != nil {
		t.Fatal(err)
	}
	if diff := gocmp.Diff(indexed["cues-front.mkv"], got); diff != "" {
		t.Errorf("keyframes walked over HTTP (-file +fetched):\n%s", diff)
	}
}
