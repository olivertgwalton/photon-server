package media

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
)

// ErrNotPublic is media a provider named at an address the server does not fetch for it: one on
// the server's own networks other than the provider's own host.
var ErrNotPublic = errors.New("media: a provider's media is fetched only from public addresses or the provider's own host")

// relayed are the headers of what the relay fetches that it passes on: what says which bytes they
// are, as serveRemote passes on.
var relayed = []string{"Accept-Ranges", "Content-Length", "Content-Range", "Content-Type", "ETag", "Last-Modified"}

// Relay serves, on a loopback port of the node's own, media a provider named at an address, to the
// tools that read it and to the server's own fetches of it. Neither sees the address, which may
// carry a provider's key, nor fetches it themselves: the relay fetches it only from public
// addresses, or from the provider's own host, which an admin chose, at every connection, a
// redirect's included, so a provider cannot have the server read its own networks.
type Relay struct {
	srv    *http.Server
	host   string
	logger *slog.Logger
	mu     sync.Mutex
	routes map[string]route
	// clients are the guarded clients by provider host, so a provider's connections are reused.
	clients sync.Map
}

type route struct {
	u    *url.URL
	from string
}

// ListenRelay starts a relay on a loopback port, serving until it is closed.
func ListenRelay(ctx context.Context, logger *slog.Logger) (*Relay, error) {
	l, err := (&net.ListenConfig{}).Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("media relay: %w", err)
	}
	r := &Relay{host: l.Addr().String(), logger: logger, routes: map[string]route{}}
	r.srv = &http.Server{Handler: http.HandlerFunc(r.serve), ReadHeaderTimeout: remoteStall}
	go func() {
		if err := r.srv.Serve(l); !errors.Is(err, http.ErrServerClosed) {
			logger.ErrorContext(ctx, "the media relay stopped: no remote media plays", slog.Any("err", err))
		}
	}()
	return r, nil
}

// Close stops the relay, and ends what it is relaying.
func (r *Relay) Close() error {
	return r.srv.Close()
}

// Relayed is the media a provider at host from offers at u, named name, as tools read it: at the
// relay, until the input is closed.
func (r *Relay) Relayed(u *url.URL, from, name string) Input {
	token := rand.Text()
	r.mu.Lock()
	r.routes[token] = route{u: u, from: from}
	r.mu.Unlock()
	return Input{URL: &url.URL{Scheme: "http", Host: r.host, Path: "/" + token}, Name: name, release: func() {
		r.mu.Lock()
		delete(r.routes, token)
		r.mu.Unlock()
	}}
}

func (r *Relay) serve(w http.ResponseWriter, req *http.Request) {
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
		// The address may carry the provider's key, so the error is told without it.
		if ue, ok := errors.AsType[*url.Error](err); ok {
			err = ue.Err
		}
		if req.Context().Err() == nil {
			r.logger.WarnContext(req.Context(), "a provider's media could not be fetched", slog.String("host", to.u.Host), slog.Any("err", err))
		}
		http.Error(w, "the media could not be fetched", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	body := bufio.NewReader(resp.Body)
	if fromStart(req) && playlist(body) {
		http.Error(w, "the media is a playlist of more addresses, which the server does not fetch", http.StatusBadGateway)
		return
	}
	for _, h := range relayed {
		if v := resp.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	// A reader that hangs up ends the copy, and there is no answer left to give it.
	if _, err := io.Copy(w, body); err != nil {
		return
	}
}

// fromStart is whether req reads the media from its first byte, as a tool does to find its format.
func fromStart(req *http.Request) bool {
	rng := req.Header.Get("Range")
	return req.Method == http.MethodGet && (rng == "" || strings.HasPrefix(rng, "bytes=0-"))
}

// playlist is whether media begins as HLS's or DASH's list of further addresses does. A tool reading
// one fetches those itself, past the relay's guard, so a provider could have it read the server's
// own networks; no container a film is in begins so.
func playlist(body *bufio.Reader) bool {
	head, err := body.Peek(512)
	if err != nil && len(head) == 0 {
		return false
	}
	head = bytes.TrimLeft(head, " \t\r\n\ufeff")
	return bytes.HasPrefix(head, []byte("#EXTM3U")) || bytes.HasPrefix(head, []byte("<"))
}

// client is the guarded client for media a provider at host from offers.
func (r *Relay) client(from string) *http.Client {
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
