package httpapi

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
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

// ParsePublicURL reads PHOTON_PUBLIC_URL, the http or https address readers reach the server's
// web app at; nil where it is not set.
func ParsePublicURL(s string) (*url.URL, error) {
	if s == "" {
		return nil, nil
	}
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("PHOTON_PUBLIC_URL is not an http or https address with a host, and no credentials, query or fragment")
	}
	return u, nil
}

// publicURL is where a reader reaches the web app: PHOTON_PUBLIC_URL, else the address this
// request came to, over HTTPS where it came that way.
func (a *API) publicURL(r *http.Request) *url.URL {
	if u := a.svc.Setup.PublicURL; u != nil {
		return u
	}
	scheme := "http"
	if overHTTPS(r, a.svc.TrustedProxies) {
		scheme = "https"
	}
	return &url.URL{Scheme: scheme, Host: r.Host}
}
