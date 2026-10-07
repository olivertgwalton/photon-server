// Package peer says where a request came from, believing a proxy only where an operator trusts it.
package peer

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strings"
)

// Proxies are the peers whose X-Forwarded-For names the client. None by default.
type Proxies []netip.Prefix

// Parse reads PHOTON_TRUSTED_PROXIES, a comma-separated list of addresses and prefixes:
// "10.0.0.0/8,127.0.0.1".
func Parse(list string) (Proxies, error) {
	out, err := Prefixes(strings.Split(list, ","))
	if err != nil {
		return nil, fmt.Errorf("PHOTON_TRUSTED_PROXIES: %w", err)
	}
	return out, nil
}

// Prefixes reads networks, each a prefix or a single address, passing over blanks.
func Prefixes(items []string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, item := range items {
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
			return nil, fmt.Errorf("%q is not an address or prefix", item)
		}
		out = append(out, netip.PrefixFrom(a, a.BitLen()))
	}
	return out, nil
}

// Client is the address a request came from. X-Forwarded-For is believed only when the direct
// peer is a trusted proxy, and then only back to the first hop that is not one, so a client cannot
// name its own address by sending the header (Emby's CVE-2021-25827).
func (p Proxies) Client(r *http.Request) netip.Addr {
	direct := Direct(r)
	if !direct.IsValid() || !p.trusts(direct) {
		return direct
	}
	hops := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	for _, hop := range slices.Backward(hops) {
		addr, err := netip.ParseAddr(strings.TrimSpace(hop))
		if err != nil {
			return direct
		}
		if addr = addr.Unmap(); !p.trusts(addr) {
			return addr
		}
	}
	return direct
}

// HTTPS is whether the client reached the server over HTTPS, here or at a trusted proxy that says
// so in X-Forwarded-Proto.
func (p Proxies) HTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return p.trusts(Direct(r)) && r.Header.Get("X-Forwarded-Proto") == "https"
}

// Local is whether an address is on this machine or one of its private networks, as Jellyfin's
// default LAN: anywhere else is remote.
func Local(a netip.Addr) bool {
	return LocalIn(nil, a)
}

// LocalIn is whether an address is on this machine or one of networks, as Jellyfin's LAN networks
// and Plex's: none is the private networks, as Local.
func LocalIn(networks []netip.Prefix, a netip.Addr) bool {
	a = a.Unmap()
	if len(networks) == 0 {
		return a.IsLoopback() || a.IsPrivate() || a.IsLinkLocalUnicast()
	}
	return a.IsLoopback() || slices.ContainsFunc(networks, func(p netip.Prefix) bool { return p.Contains(a) })
}

// Direct is the address of the connection's other end, a proxy or the client itself.
func Direct(r *http.Request) netip.Addr {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}
	}
	return addr.Unmap()
}

func (p Proxies) trusts(a netip.Addr) bool {
	return slices.ContainsFunc(p, func(prefix netip.Prefix) bool { return prefix.Contains(a) })
}
