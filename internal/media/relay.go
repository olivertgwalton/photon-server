package media

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"sync"
)

// ErrNotPublic is media a provider named at an address the server does not fetch for it: one on
// the server's own networks other than the provider's own host.
var ErrNotPublic = errors.New("media: a provider's media is fetched only from public addresses or the provider's own host")

// relayed are the headers of what the relay fetches that it passes on: what says which bytes they
// are, as serveRemote passes on.
var relayed = []string{"Accept-Ranges", "Content-Length", "Content-Range", "Content-Type", "ETag", "Last-Modified"}

// relay serves, on a loopback port of the node's own, media a provider named at an address, to the
// tools that read it and to the server's own fetches of it. Neither sees the address, which may
// carry a provider's key, nor fetches it themselves: the relay fetches it only from public
// addresses, or from the provider's own host, which an admin chose, at every connection, a
// redirect's included, so a provider cannot have the server read its own networks.
type relay struct {
	start  sync.Once
	base   string
	err    error
	mu     sync.Mutex
	routes map[string]route
	// clients are the guarded clients by provider host, so a provider's connections are reused.
	clients sync.Map
}

type route struct {
	u    *url.URL
	from string
}

var relays = &relay{routes: map[string]route{}}

// Relayed is the media a provider at host from offers at u, named name, as tools read it: at the
// relay, until the input is closed.
func Relayed(u *url.URL, from, name string) (Input, error) {
	return relays.add(u, from, name)
}

func (r *relay) add(u *url.URL, from, name string) (Input, error) {
	r.start.Do(r.listen)
	token := rand.Text()
	r.mu.Lock()
	err := r.err
	if err == nil {
		r.routes[token] = route{u: u, from: from}
	}
	r.mu.Unlock()
	if err != nil {
		return Input{}, err
	}
	at, err := url.Parse(r.base + "/" + token)
	if err != nil {
		return Input{}, err
	}
	return Input{URL: at, Name: name, release: func() {
		r.mu.Lock()
		delete(r.routes, token)
		r.mu.Unlock()
	}}, nil
}

func (r *relay) listen() {
	l, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		r.err = fmt.Errorf("media relay: %w", err)
		return
	}
	r.base = "http://" + l.Addr().String()
	srv := &http.Server{Handler: http.HandlerFunc(r.serve), ReadHeaderTimeout: remoteStall}
	go func() {
		// The relay serves for as long as the server runs; one that stops relays nothing more.
		err := srv.Serve(l)
		r.mu.Lock()
		r.err = fmt.Errorf("media relay: %w", err)
		r.mu.Unlock()
	}()
}

func (r *relay) serve(w http.ResponseWriter, req *http.Request) {
	r.mu.Lock()
	to, ok := r.routes[req.URL.Path[1:]]
	r.mu.Unlock()
	if !ok || req.Method != http.MethodGet && req.Method != http.MethodHead {
		http.NotFound(w, req)
		return
	}
	header := http.Header{}
	for _, h := range []string{"Range", "If-Range"} {
		if v := req.Header.Get(h); v != "" {
			header.Set(h, v)
		}
	}
	resp, err := fetch(req.Context(), r.client(to.from), req.Method, to.u, header)
	if err != nil {
		http.Error(w, "the media could not be fetched", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	for _, h := range relayed {
		if v := resp.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	// A reader that hangs up ends the copy, and there is no answer left to give it.
	if _, err := io.Copy(w, resp.Body); err != nil {
		return
	}
}

// client is the guarded client for media a provider at host from offers.
func (r *relay) client(from string) *http.Client {
	if c, ok := r.clients.Load(from); ok {
		if client, ok := c.(*http.Client); ok {
			return client
		}
	}
	dialer := &net.Dialer{Timeout: remoteStall}
	c := &http.Client{Transport: &http.Transport{
		TLSHandshakeTimeout:   remoteStall,
		ResponseHeaderTimeout: remoteStall,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			if address == from {
				return dialer.DialContext(ctx, network, address)
			}
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
			if err != nil {
				return nil, err
			}
			for _, ip := range ips {
				if !Public(ip) {
					return nil, fmt.Errorf("%w: %s", ErrNotPublic, host)
				}
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
		},
	}}
	r.clients.Store(from, c)
	return c
}

// cgnat is the shared address space carriers put behind one public address, as private to them as
// 10.0.0.0/8 is to a household.
var cgnat = netip.MustParsePrefix("100.64.0.0/10")

// Public reports whether ip is an address on the internet at large, not the server's own or its
// networks'.
func Public(ip netip.Addr) bool {
	ip = ip.Unmap()
	return ip.IsGlobalUnicast() && !ip.IsPrivate() && !cgnat.Contains(ip)
}
