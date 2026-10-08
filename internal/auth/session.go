package auth

import (
	"context"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

type sessionKey struct{}

// WithSession is ctx carrying the session its request was signed in as.
func WithSession(ctx context.Context, s domain.Session) context.Context {
	return context.WithValue(ctx, sessionKey{}, s)
}

// SessionOf is the session ctx carries: none outside a route that requires one.
func SessionOf(ctx context.Context) domain.Session {
	s, _ := ctx.Value(sessionKey{}).(domain.Session)
	return s
}
