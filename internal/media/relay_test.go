package media

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// listen is a relay of the test's own, telling log what it could not fetch.
func listen(t *testing.T, log io.Writer) *Relay {
	t.Helper()
	r, err := ListenRelay(t.Context(), slog.New(slog.NewTextHandler(log, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	return r
}

func get(t *testing.T, in Input, rng string) (int, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, in.URL.String(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if rng != "" {
		req.Header.Set("Range", rng)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, string(b)
}

// Media a provider names is read at the relay, by range, never from its own address; from its
// own host, or a public one, and nowhere else on the server's networks, a redirect's included. One
// refused is told an admin by its host, never its address, which carries the provider's key.
func TestAProvidersMediaIsRelayedFromItsOwnHostOrAPublicOne(t *testing.T) {
	body := strings.Repeat("0123456789", 100)
	media := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("key") != "s3cret" {
			http.Error(w, "no key", http.StatusUnauthorized)
			return
		}
		http.ServeContent(w, r, "film.mkv", time.Time{}, strings.NewReader(body))
	}))
	defer media.Close()
	at, err := url.Parse(media.URL + "/film.mkv?key=s3cret")
	if err != nil {
		t.Fatal(err)
	}
	from := strings.TrimPrefix(media.URL, "http://")
	var log bytes.Buffer
	relay := listen(t, &log)

	in := relay.Relayed(at, from, "Heat (1995).mkv")
	if strings.Contains(in.URL.String(), "s3cret") || in.Name != "Heat (1995).mkv" {
		t.Errorf("relayed as %s named %q, want an address with no key", in.URL, in.Name)
	}
	if code, got := get(t, in, "bytes=10-19"); code != http.StatusPartialContent || got != body[10:20] {
		t.Errorf("a range answered %d %q, want bytes 10-19", code, got)
	}

	// Named by an addon elsewhere, the media on the server's own network is not fetched, nor is it
	// through a redirect from an address that is allowed.
	elsewhere := relay.Relayed(at, "addon.example:443", "Heat (1995).mkv")
	if code, _ := get(t, elsewhere, ""); code != http.StatusBadGateway {
		t.Errorf("media on a private address named by another host's addon answered %d, want 502", code)
	}
	if told := log.String(); !strings.Contains(told, "level=WARN") || !strings.Contains(told, at.Host) || !strings.Contains(told, ErrNotPublic.Error()) || strings.Contains(told, "s3cret") {
		t.Errorf("refused, the relay logged %q; want a warning of the host and why, without the key", told)
	}
	redirect := httptest.NewServer(http.RedirectHandler(at.String(), http.StatusFound))
	defer redirect.Close()
	to, err := url.Parse(redirect.URL)
	if err != nil {
		t.Fatal(err)
	}
	redirected := relay.Relayed(to, strings.TrimPrefix(redirect.URL, "http://"), "Heat (1995).mkv")
	if code, _ := get(t, redirected, ""); code != http.StatusBadGateway {
		t.Errorf("redirected to a private address answered %d, want 502", code)
	}

	if err := in.Close(); err != nil {
		t.Fatal(err)
	}
	if code, _ := get(t, in, ""); code != http.StatusNotFound {
		t.Errorf("closed, the relay answered %d, want 404", code)
	}
}

// playlistAt serves, as a provider's media, a list naming a segment at internal, which counts each
// request it is sent. Its type is HLS's, without which FFmpeg reads no list at an address with no
// .m3u8 in it, as the relay's has none.
func playlistAt(t *testing.T, list string) (Input, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	internal := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	t.Cleanup(internal.Close)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		if _, err := io.WriteString(w, strings.ReplaceAll(list, "INTERNAL", internal.URL)); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(provider.Close)
	at, err := url.Parse(provider.URL + "/stream")
	if err != nil {
		t.Fatal(err)
	}
	in := listen(t, io.Discard).Relayed(at, strings.TrimPrefix(provider.URL, "http://"), "Heat (1995).mkv")
	t.Cleanup(func() { in.Close() })
	return in, &hits
}

const (
	hlsList  = "#EXTM3U\n#EXT-X-TARGETDURATION:6\n#EXTINF:6,\nINTERNAL/secret.ts\n#EXT-X-ENDLIST\n"
	dashList = "\ufeff<?xml version=\"1.0\"?>\n<MPD><Period><BaseURL>INTERNAL/</BaseURL></Period></MPD>\n"
)

// A provider answering with a playlist would have whatever reads it fetch the addresses it lists
// itself, past the relay's guard, so the relay refuses it.
func TestARelayedPlaylistIsRefused(t *testing.T) {
	for name, list := range map[string]string{"HLS": hlsList, "DASH": dashList} {
		in, _ := playlistAt(t, list)
		for _, rng := range []string{"", "bytes=0-"} {
			if code, _ := get(t, in, rng); code != http.StatusBadGateway {
				t.Errorf("%s from %q answered %d, want 502", name, rng, code)
			}
		}
	}
}

// ffprobe reading a provider's playlist through the relay fetches nothing it lists.
func TestAToolReadingARelayedPlaylistFetchesNothingItLists(t *testing.T) {
	tools := ffprobe(t)
	in, hits := playlistAt(t, hlsList)
	if _, err := tools.Probe(t.Context(), in); err == nil {
		t.Error("a playlist was probed as media")
	}
	if n := hits.Load(); n != 0 {
		t.Errorf("ffprobe fetched an address the playlist listed %d times", n)
	}
}

func TestOnlyTheInternetAtLargeIsPublic(t *testing.T) {
	for addr, want := range map[string]bool{
		"8.8.8.8": true, "2606:4700::1111": true,
		"127.0.0.1": false, "10.0.0.5": false, "192.168.1.2": false, "172.16.0.1": false, "169.254.169.254": false,
		"100.64.0.1": false, "0.0.0.0": false, "::1": false, "fd00::1": false, "fe80::1": false, "::ffff:10.0.0.1": false,
	} {
		if got := Public(netip.MustParseAddr(addr)); got != want {
			t.Errorf("%s public: %v, want %v", addr, got, want)
		}
	}
}
