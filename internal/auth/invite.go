package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// InviteTTL is how long an invite may be accepted: long enough to reach someone by a message they
// read days later.
const InviteTTL = 7 * 24 * time.Hour

// Invite makes an invite to choose the first password of a profile by keeps, answering its code.
// The code is sent as a link, never typed, so it has a token's 256 bits, not a typed code's few.
func (s *Service) Invite(ctx context.Context, profile uuid.UUID, by *uuid.UUID) (string, error) {
	b := make([]byte, 32)
	rand.Read(b)
	code := base64.RawURLEncoding.EncodeToString(b)
	now := time.Now()
	return code, s.store.AddInvite(ctx, profile, by, hashToken(code), now, now.Add(InviteTTL))
}

// AcceptInvite sets the password an invite's holder chose on its profile and signs in the device
// they chose it on.
func (s *Service) AcceptInvite(ctx context.Context, code, password string, device Device) (string, domain.Profile, error) {
	hash, err := s.HashPassword(ctx, password)
	if err != nil {
		return "", domain.Profile{}, err
	}
	profile, err := s.store.AcceptInvite(ctx, hashToken(code), hash, time.Now())
	if err != nil {
		return "", domain.Profile{}, err
	}
	token, err := s.startSession(ctx, profile, device, domain.SignInIdentity{})
	return token, profile, err
}
