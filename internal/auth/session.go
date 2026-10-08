package auth

import (
	"context"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/kv"
)

// Authenticator signs devices in and out, by password or by pairing, and says who a token is.
type Authenticator interface {
	SignIn(ctx context.Context, name, password string, device Device) (string, domain.Profile, error)
	Authenticate(ctx context.Context, token string) (domain.Session, error)
	SignOut(ctx context.Context, session uuid.UUID) error
	StartPairing(ctx context.Context, d Device, style CodeStyle) (PairingStart, error)
	ApprovePairing(ctx context.Context, approver domain.Session, userCode string) (Device, error)
	PollPairing(ctx context.Context, deviceCode string) (kv.PairingState, string, domain.Profile, error)
}

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
