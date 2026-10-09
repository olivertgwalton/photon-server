package media

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"
	"time"
)

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
// own host, or a public one, and nowhere else on the server's networks, a redirect's included.
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

	in, err := Relayed(at, from, "Heat (1995).mkv")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(in.URL.String(), "s3cret") || in.Name != "Heat (1995).mkv" {
		t.Errorf("relayed as %s named %q, want an address with no key", in.URL, in.Name)
	}
	if code, got := get(t, in, "bytes=10-19"); code != http.StatusPartialContent || got != body[10:20] {
		t.Errorf("a range answered %d %q, want bytes 10-19", code, got)
	}

	// Named by an addon elsewhere, the media on the server's own network is not fetched, nor is it
	// through a redirect from an address that is allowed.
	elsewhere, err := Relayed(at, "addon.example:443", "Heat (1995).mkv")
	if err != nil {
		t.Fatal(err)
	}
	if code, _ := get(t, elsewhere, ""); code != http.StatusBadGateway {
		t.Errorf("media on a private address named by another host's addon answered %d, want 502", code)
	}
	redirect := httptest.NewServer(http.RedirectHandler(at.String(), http.StatusFound))
	defer redirect.Close()
	to, err := url.Parse(redirect.URL)
	if err != nil {
		t.Fatal(err)
	}
	redirected, err := Relayed(to, strings.TrimPrefix(redirect.URL, "http://"), "Heat (1995).mkv")
	if err != nil {
		t.Fatal(err)
	}
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
