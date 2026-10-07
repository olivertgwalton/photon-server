package jellyfin

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/follow"
)

const (
	// rereadEvery also retries a port this node could not have, which may since have been freed.
	rereadEvery = time.Minute
	// shutdownGrace is how long requests have to finish when the port changes or the API is turned
	// off, as photon's own listener gives them.
	shutdownGrace = 10 * time.Second
)

type settings interface {
	Network(ctx context.Context) (domain.Network, error)
}

// Listener serves the API on the port an admin sets while they have it on, on this node, taking up
// a change as every node is told of it.
type Listener struct {
	settings  settings
	subscribe func() (<-chan domain.Event, func())
	handler   http.Handler
	// host is the address photon's own API is bound to, so this API is no more exposed than it.
	host string
	// secure serves HTTPS beside plain HTTP on one port, as photon's own port does.
	secure    func(net.Listener) net.Listener
	tlsConfig *tls.Config
	log       *slog.Logger
	err       atomic.Pointer[error]
}

// NewListener serves h beside photon's own API at listen, an address as PHOTON_LISTEN gives it.
func NewListener(s settings, subscribe func() (<-chan domain.Event, func()), h http.Handler, listen string,
	secure func(net.Listener) net.Listener, tlsConfig *tls.Config, log *slog.Logger,
) *Listener {
	host, _, _ := net.SplitHostPort(listen)
	return &Listener{settings: s, subscribe: subscribe, handler: h, host: host, secure: secure, tlsConfig: tlsConfig, log: log}
}

// Err is why this node is not serving the API on the port set, nil while it is or it is off.
func (l *Listener) Err() error {
	if err := l.err.Load(); err != nil {
		return *err
	}
	return nil
}

// served is the API being served on a port.
type served struct {
	port int
	srv  *http.Server
	done chan struct{}
}

// Run serves what is set until ctx ends, reading it again as an admin changes it and every
// rereadEvery.
func (l *Listener) Run(ctx context.Context) {
	var cur *served
	defer func() { cur.stop(ctx) }()
	follow.Events(ctx, l.subscribe, rereadEvery, func(ctx context.Context) { cur = l.apply(ctx, cur) },
		domain.EventNetworkChanged)
}

// apply serves the API as set, on cur still where that is unchanged. What cannot be read leaves
// the node serving as it was.
func (l *Listener) apply(ctx context.Context, cur *served) *served {
	n, err := l.settings.Network(ctx)
	if err != nil {
		if ctx.Err() == nil {
			l.log.WarnContext(ctx, "jellyfin settings not read", slog.Any("err", err))
		}
		return cur
	}
	port := 0
	switch n.Jellyfin {
	case domain.JellyfinOn:
		port = n.JellyfinPort
	case domain.JellyfinOff:
	}
	if cur != nil && cur.port == port && !cur.ended() {
		return cur
	}
	// Stopped before the next is bound, which may be on the same port.
	cur.stop(ctx)
	l.err.Store(nil)
	if port == 0 {
		return nil
	}
	next, err := l.serve(ctx, port)
	if err != nil {
		l.err.Store(&err)
		l.log.WarnContext(ctx, "jellyfin's apps cannot reach this node", slog.Int("port", port), slog.Any("err", err))
		return nil
	}
	l.log.InfoContext(ctx, "serving jellyfin's api", slog.Int("port", port))
	return next
}

func (l *Listener) serve(ctx context.Context, port int) (*served, error) {
	ln, err := new(net.ListenConfig).Listen(ctx, "tcp", net.JoinHostPort(l.host, strconv.Itoa(port)))
	if err != nil {
		return nil, fmt.Errorf("port %d: %w", port, err)
	}
	s := &served{port: port, done: make(chan struct{}), srv: &http.Server{
		Handler: l.handler, TLSConfig: l.tlsConfig,
		ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 2 * time.Minute,
		ErrorLog: slog.NewLogLogger(l.log.Handler(), slog.LevelWarn),
	}}
	go func() {
		defer close(s.done)
		if err := s.srv.Serve(l.secure(ln)); !errors.Is(err, http.ErrServerClosed) {
			l.err.Store(&err)
			l.log.WarnContext(ctx, "jellyfin's api stopped", slog.Any("err", err))
		}
	}()
	return s, nil
}

func (s *served) ended() bool {
	select {
	case <-s.done:
		return true
	default:
		return false
	}
}

// stop gives open requests shutdownGrace to finish, then closes what is left, whether or not ctx
// has ended.
func (s *served) stop(ctx context.Context) {
	if s == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownGrace)
	defer cancel()
	if err := s.srv.Shutdown(ctx); err != nil {
		_ = s.srv.Close()
	}
	<-s.done
}
