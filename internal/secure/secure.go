// Package secure serves HTTPS and plain HTTP on one port, as Plex's 32400 does, in the mode and
// with the certificate an admin sets, which every node takes up at once.
package secure

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/follow"
)

const (
	// rereadEvery also takes up a certificate renewed in place, as certbot's and Tailscale's are.
	rereadEvery = time.Minute
	// sniffWithin is how long a connection has to send its first byte, which says whether it
	// speaks TLS, before it is closed.
	sniffWithin = 10 * time.Second
	// tlsHandshake is the record type a TLS connection's first byte is.
	tlsHandshake = 0x16
)

var errNoCertificate = errors.New("this node holds no certificate")

type settings interface {
	Network(ctx context.Context) (domain.Network, error)
}

// Server is what this node serves: disabled until a mode and its certificate are applied.
type Server struct {
	settings  settings
	subscribe func() (<-chan domain.Event, func())
	log       *slog.Logger
	state     atomic.Pointer[state]
	config    *tls.Config
}

type state struct {
	mode domain.SecureConnections
	cert *tls.Certificate
}

func New(s settings, subscribe func() (<-chan domain.Event, func()), log *slog.Logger) *Server {
	srv := &Server{settings: s, subscribe: subscribe, log: log}
	srv.state.Store(&state{mode: domain.SecureDisabled})
	srv.config = &tls.Config{
		NextProtos: []string{"h2", "http/1.1"},
		GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
			if cert := srv.state.Load().cert; cert != nil {
				return cert, nil
			}
			return nil, errNoCertificate
		},
	}
	return srv
}

// Load reads a network's certificate, refusing one it needs and cannot read; nil when disabled.
func Load(n domain.Network) (*tls.Certificate, error) {
	if err := n.Check(); err != nil {
		return nil, err
	}
	switch n.Secure {
	case domain.SecureDisabled:
		return nil, nil
	case domain.SecureRequired, domain.SecurePreferred:
	}
	cert, err := tls.LoadX509KeyPair(n.Certificate, n.Key)
	if err != nil {
		return nil, fmt.Errorf("the certificate and key cannot be read: %w", err)
	}
	return &cert, nil
}

// Mode is how this node serves: disabled while it holds no certificate, so Required never sends
// a request to an HTTPS this node cannot answer.
func (s *Server) Mode() domain.SecureConnections {
	return s.state.Load().mode
}

// Scheme is the scheme a client is told to reach this node by.
func (s *Server) Scheme() string {
	switch s.Mode() {
	case domain.SecureRequired, domain.SecurePreferred:
		return "https"
	case domain.SecureDisabled:
	}
	return "http"
}

// Run keeps the node serving what is set until ctx ends, reading it again as an admin changes it
// and every rereadEvery.
func (s *Server) Run(ctx context.Context) {
	follow.Events(ctx, s.subscribe, rereadEvery, s.reread, domain.EventNetworkChanged)
}

// reread applies what is set. What cannot be read leaves the node serving as it was, so a
// certificate caught mid-renewal is served until its files are whole again.
func (s *Server) reread(ctx context.Context) {
	n, err := s.settings.Network(ctx)
	var cert *tls.Certificate
	if err == nil {
		cert, err = Load(n)
	}
	if err != nil {
		if ctx.Err() == nil {
			s.log.WarnContext(ctx, "secure connections not applied", slog.Any("err", err))
		}
		return
	}
	s.state.Store(&state{mode: n.Secure, cert: cert})
}

// Listen answers HTTPS and plain HTTP both on l: a connection whose first byte begins a TLS
// handshake is served TLS, with the certificate held when it shakes hands, or closed while none
// is held, so a client probing HTTPS first moves on to HTTP at once and nothing is logged. Each
// is sniffed in a goroutine of its own, so a client slow to speak holds up no other.
func (s *Server) Listen(l net.Listener) net.Listener {
	e := &either{Listener: l, server: s, accepted: make(chan accepted), closed: make(chan struct{})}
	go e.run()
	return e
}

// TLSConfig is the configuration Listen serves TLS with, which an http.Server is given so it
// speaks HTTP/2 over those connections.
func (s *Server) TLSConfig() *tls.Config {
	return s.config
}

type either struct {
	net.Listener
	server   *Server
	accepted chan accepted
	closed   chan struct{}
	close    sync.Once
}

type accepted struct {
	conn net.Conn
	err  error
}

func (e *either) run() {
	for {
		c, err := e.Listener.Accept()
		if err != nil {
			select {
			case e.accepted <- accepted{err: err}:
			case <-e.closed:
				return
			}
			if errors.Is(err, net.ErrClosed) {
				return
			}
			continue
		}
		go e.sniff(c)
	}
}

func (e *either) sniff(c net.Conn) {
	_ = c.SetReadDeadline(time.Now().Add(sniffWithin))
	r := bufio.NewReader(c)
	first, err := r.Peek(1)
	_ = c.SetReadDeadline(time.Time{})
	if err != nil {
		c.Close()
		return
	}
	var conn net.Conn = peeked{Conn: c, r: r}
	if first[0] == tlsHandshake {
		if e.server.state.Load().cert == nil {
			c.Close()
			return
		}
		conn = tls.Server(conn, e.server.config)
	}
	select {
	case e.accepted <- accepted{conn: conn}:
	case <-e.closed:
		conn.Close()
	}
}

func (e *either) Accept() (net.Conn, error) {
	select {
	case a := <-e.accepted:
		return a.conn, a.err
	case <-e.closed:
		return nil, net.ErrClosed
	}
}

func (e *either) Close() error {
	e.close.Do(func() { close(e.closed) })
	return e.Listener.Close()
}

// peeked is a connection whose first bytes were read to sniff it, and are read again.
type peeked struct {
	net.Conn
	r *bufio.Reader
}

func (p peeked) Read(b []byte) (int, error) { return p.r.Read(b) }
