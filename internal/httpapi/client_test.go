package httpapi

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

func TestClientAddr(t *testing.T) {
	proxy := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
	tests := []struct {
		name    string
		peer    string
		xff     []string
		trusted []netip.Prefix
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
			if got := clientAddr(r, tt.trusted); got.String() != tt.want {
				t.Errorf("clientAddr = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestParseTrustedProxies(t *testing.T) {
	got, err := ParseTrustedProxies(" 10.0.0.0/8, 127.0.0.1,::1 ,172.16.5.4/12")
	if err != nil {
		t.Fatal(err)
	}
	want := "[10.0.0.0/8 127.0.0.1/32 ::1/128 172.16.0.0/12]"
	if s := fmt.Sprint(got); s != want {
		t.Errorf("parsed %s, want %s", s, want)
	}
	if empty, err := ParseTrustedProxies(""); err != nil || len(empty) != 0 {
		t.Errorf("an empty list trusts %v (err %v), want nothing", empty, err)
	}
	if _, err := ParseTrustedProxies("10.0.0.0/8,proxy.local"); err == nil {
		t.Error("a host name was accepted as a proxy")
	}
}
