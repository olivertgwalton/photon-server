package peer

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

func TestClient(t *testing.T) {
	proxy := Proxies{netip.MustParsePrefix("10.0.0.0/8")}
	tests := []struct {
		name    string
		peer    string
		xff     []string
		trusted Proxies
		want    string
	}{
		{"no proxy is trusted: the header is ignored", "203.0.113.9:5000", []string{"127.0.0.1"}, nil, "203.0.113.9"},
		{"an untrusted peer cannot claim the LAN", "203.0.113.9:5000", []string{"192.168.1.2"}, proxy, "203.0.113.9"},
		{"a trusted proxy names the client", "10.0.0.2:5000", []string{"198.51.100.7"}, proxy, "198.51.100.7"},
		{"a client's own forged hop is left of the proxy's", "10.0.0.2:5000", []string{"127.0.0.1, 198.51.100.7"}, proxy, "198.51.100.7"},
		{"a chain of trusted proxies", "10.0.0.2:5000", []string{"198.51.100.7, 10.0.0.9"}, proxy, "198.51.100.7"},
		{"headers split over several lines", "10.0.0.2:5000", []string{"127.0.0.1", "198.51.100.7"}, proxy, "198.51.100.7"},
		{"garbage in the header falls back to the peer", "10.0.0.2:5000", []string{"not-an-address"}, proxy, "10.0.0.2"},
		{"IPv6", "[2001:db8::1]:5000", nil, nil, "2001:db8::1"},
		{"IPv4 mapped in IPv6", "[::ffff:203.0.113.9]:5000", nil, nil, "203.0.113.9"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.RemoteAddr = tt.peer
			for _, v := range tt.xff {
				r.Header.Add("X-Forwarded-For", v)
			}
			if got := tt.trusted.Client(r); got.String() != tt.want {
				t.Errorf("Client = %s, want %s", got, tt.want)
			}
		})
	}
}

// Networks an admin lists are read as prefixes, an address alone as its own, blanks passed over.
func TestPrefixes(t *testing.T) {
	got, err := Prefixes([]string{" 10.0.0.0/8", " 127.0.0.1", "::1 ", "172.16.5.4/12", ""})
	if err != nil {
		t.Fatal(err)
	}
	want := "[10.0.0.0/8 127.0.0.1/32 ::1/128 172.16.0.0/12]"
	if s := fmt.Sprint(got); s != want {
		t.Errorf("parsed %s, want %s", s, want)
	}
	if empty, err := Prefixes(nil); err != nil || len(empty) != 0 {
		t.Errorf("an empty list trusts %v (err %v), want nothing", empty, err)
	}
	if _, err := Prefixes([]string{"10.0.0.0/8", "proxy.local"}); err == nil {
		t.Error("a host name was accepted as a proxy")
	}
}

// This machine and its private networks are local; anywhere else, and CGNAT, is remote.
func TestLocal(t *testing.T) {
	for addr, want := range map[string]bool{
		"127.0.0.1":        true,
		"::1":              true,
		"10.1.2.3":         true,
		"172.20.0.5":       true,
		"192.168.1.20":     true,
		"169.254.10.1":     true,
		"fd12:3456::1":     true,
		"fe80::1%en0":      true,
		"::ffff:192.0.2.1": false,
		"::ffff:10.0.0.9":  true,
		"8.8.8.8":          false,
		"100.64.0.1":       false,
		"2001:4860::8888":  false,
	} {
		if got := Local(netip.MustParseAddr(addr)); got != want {
			t.Errorf("Local(%s) = %t, want %t", addr, got, want)
		}
	}
}

// Networks set are local in place of the private ones; this machine always is.
func TestLocalIn(t *testing.T) {
	tailnet := []netip.Prefix{netip.MustParsePrefix("100.64.0.0/10")}
	for addr, want := range map[string]bool{
		"100.101.102.103": true,
		"192.168.1.20":    false,
		"127.0.0.1":       true,
		"::1":             true,
		"203.0.113.9":     false,
	} {
		if got := LocalIn(tailnet, netip.MustParseAddr(addr)); got != want {
			t.Errorf("LocalIn(tailnet, %s) = %t, want %t", addr, got, want)
		}
	}
}
