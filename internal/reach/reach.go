// Package reach keeps how clients reach the server, as an admin sets it: its address outside, the
// proxies trusted to say who a client is, and whether it answers clients looking for it. Every
// node takes up a change at once.
package reach

import (
	"context"
	"log/slog"
	"net/http"
	"net/netip"
	"net/url"
	"sync/atomic"
	"time"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/follow"
	"github.com/olivertgwalton/photon-server/internal/peer"
)

// rereadEvery takes up a change whose event was lost with Valkey's connection.
const rereadEvery = time.Minute

type settings interface {
	Network(ctx context.Context) (domain.Network, error)
}

// Reach is how the server is reached now. A nil Reach trusts no proxy, has no address outside,
// and answers no one looking for it.
type Reach struct {
	settings  settings
	subscribe func() (<-chan domain.Event, func())
	log       *slog.Logger
	state     atomic.Pointer[state]
}

type state struct {
	proxies   peer.Proxies
	public    *url.URL
	discovery domain.Discovery
}

// New reads how the server is reached, so the first request is answered by it.
func New(ctx context.Context, s settings, subscribe func() (<-chan domain.Event, func()), log *slog.Logger) (*Reach, error) {
	r := &Reach{settings: s, subscribe: subscribe, log: log}
	n, err := s.Network(ctx)
	if err != nil {
		return nil, err
	}
	r.apply(n)
	return r, nil
}

// Run keeps it as an admin sets it until ctx ends.
func (r *Reach) Run(ctx context.Context) {
	follow.Events(ctx, r.subscribe, rereadEvery, r.reread, domain.EventNetworkChanged)
}

func (r *Reach) reread(ctx context.Context) {
	n, err := r.settings.Network(ctx)
	if err != nil {
		if ctx.Err() == nil {
			r.log.WarnContext(ctx, "how the server is reached not read", slog.Any("err", err))
		}
		return
	}
	r.apply(n)
}

func (r *Reach) apply(n domain.Network) {
	s := &state{proxies: n.TrustedProxies, discovery: n.Discovery}
	if n.PublicURL != "" {
		u, err := url.Parse(n.PublicURL)
		if err != nil {
			r.log.Warn("the server's address outside not read", slog.Any("err", err))
		}
		s.public = u
	}
	r.state.Store(s)
}

func (r *Reach) now() *state {
	if r == nil {
		return &state{discovery: domain.DiscoveryOff}
	}
	return r.state.Load()
}

// Proxies are the peers whose X-Forwarded-For names the client.
func (r *Reach) Proxies() peer.Proxies { return r.now().proxies }

// Client is the client a request came from, as a trusted proxy says.
func (r *Reach) Client(req *http.Request) netip.Addr { return r.now().proxies.Client(req) }

// HTTPS is whether a request reached the server over HTTPS, as a trusted proxy says.
func (r *Reach) HTTPS(req *http.Request) bool { return r.now().proxies.HTTPS(req) }

// Untrusted is whether a request names its client through a proxy that is not trusted.
func (r *Reach) Untrusted(req *http.Request) bool { return r.now().proxies.Untrusted(req) }

// PublicURL is where readers reach the web app from outside; nil where none is set.
func (r *Reach) PublicURL() *url.URL { return r.now().public }

// Discovery is whether the server answers clients looking for it.
func (r *Reach) Discovery() domain.Discovery { return r.now().discovery }
