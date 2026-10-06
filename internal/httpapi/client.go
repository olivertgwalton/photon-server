package httpapi

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strings"
)

// clientAddr is the address a request came from. X-Forwarded-For is believed only when the direct
// peer is a trusted proxy, and then only back to the first hop that is not one, so a client cannot
// name its own address by sending the header (Emby's CVE-2021-25827).
func clientAddr(r *http.Request, trusted []netip.Prefix) netip.Addr {
	peer := peerAddr(r)
	if !peer.IsValid() || !isTrusted(peer, trusted) {
		return peer
	}
	hops := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	for _, hop := range slices.Backward(hops) {
		addr, err := netip.ParseAddr(strings.TrimSpace(hop))
		if err != nil {
			return peer
		}
		if addr = addr.Unmap(); !isTrusted(addr, trusted) {
			return addr
		}
	}
	return peer
}

func peerAddr(r *http.Request) netip.Addr {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}
	}
	return peer.Unmap()
}

// overHTTPS is whether the browser reached the server over HTTPS, here or at a trusted proxy that
// says so in X-Forwarded-Proto.
func overHTTPS(r *http.Request, trusted []netip.Prefix) bool {
	if r.TLS != nil {
		return true
	}
	return isTrusted(peerAddr(r), trusted) && r.Header.Get("X-Forwarded-Proto") == "https"
}

func isTrusted(a netip.Addr, trusted []netip.Prefix) bool {
	return slices.ContainsFunc(trusted, func(p netip.Prefix) bool { return p.Contains(a) })
}

// ParseTrustedProxies reads a comma-separated list of addresses and prefixes: "10.0.0.0/8,127.0.0.1".
func ParseTrustedProxies(list string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for item := range strings.SplitSeq(list, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if p, err := netip.ParsePrefix(item); err == nil {
			out = append(out, p.Masked())
			continue
		}
		a, err := netip.ParseAddr(item)
		if err != nil {
			return nil, fmt.Errorf("PHOTON_TRUSTED_PROXIES: %q is not an address or prefix", item)
		}
		out = append(out, netip.PrefixFrom(a, a.BitLen()))
	}
	return out, nil
}
