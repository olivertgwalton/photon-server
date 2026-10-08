// Package identity keeps what the server is called and what its metadata is asked in, as an admin
// sets them. Every node takes up a change at once.
package identity

import (
	"cmp"
	"context"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/follow"
)

// rereadEvery takes up a change whose event was lost with Valkey's connection.
const rereadEvery = time.Minute

type settings interface {
	ServerSettings(ctx context.Context) (domain.ServerSettings, error)
}

// Server is the server as it is set now.
type Server struct {
	settings settings
	host     string
	log      *slog.Logger
	set      atomic.Pointer[domain.ServerSettings]
}

// New reads the server as it is set, so the first request is answered by it. host is this node's,
// the server's name where an admin sets none.
func New(ctx context.Context, s settings, host string, log *slog.Logger) (*Server, error) {
	set, err := s.ServerSettings(ctx)
	if err != nil {
		return nil, err
	}
	srv := &Server{settings: s, host: host, log: log}
	srv.set.Store(&set)
	return srv, nil
}

// Run keeps it as an admin sets it until ctx ends, as subscribe tells of a change.
func (s *Server) Run(ctx context.Context, subscribe func() (<-chan domain.Event, func())) {
	follow.Events(ctx, subscribe, rereadEvery, s.reread, domain.EventServerChanged)
}

func (s *Server) reread(ctx context.Context) {
	set, err := s.settings.ServerSettings(ctx)
	if err != nil {
		if ctx.Err() == nil {
			s.log.WarnContext(ctx, "the server's settings not read", slog.Any("err", err))
		}
		return
	}
	s.set.Store(&set)
}

// Name is what the server is called.
func (s *Server) Name() string { return cmp.Or(s.set.Load().Name, s.host) }

// Locale is what its metadata is asked in where a library or title says none.
func (s *Server) Locale() domain.Locale { return s.set.Load().Locale }
