package auth

import (
	"context"
	"errors"
	"strings"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/kv"
	"github.com/olivertgwalton/photon-server/internal/store"
)

const (
	// idleExpiry keeps a television signed in through a long absence; each use slides it.
	idleExpiry = 90 * 24 * time.Hour
	// touchEvery limits how often a session's use is written: once an hour, not every request.
	touchEvery = time.Hour
	// concurrentHashes bounds sign-ins hashing at once, at 64 MiB each.
	concurrentHashes = 2
	minPasswordLen   = 8
)

var (
	ErrInvalidCredentials = errors.New("the name or password is wrong")
	ErrUnauthenticated    = errors.New("no valid device token")
	ErrPasswordTooShort   = errors.New("a password needs at least 8 characters")
)

type Service struct {
	store  *store.Store
	kv     *kv.KV
	hasher *hasher
	// dummy is verified against when no profile has the name, so a sign-in for an unknown name
	// takes as long as one for a known name.
	dummy string
}

func New(ctx context.Context, st *store.Store, k *kv.KV) (*Service, error) {
	h := newHasher(concurrentHashes)
	dummy, err := h.Hash(ctx, uuid.NewV4().String())
	if err != nil {
		return nil, err
	}
	return &Service{store: st, kv: k, hasher: h, dummy: dummy}, nil
}

// HashPassword hashes a new password for storing, refusing one too short to be one.
func HashPassword(ctx context.Context, password string) (string, error) {
	if len([]rune(password)) < minPasswordLen {
		return "", ErrPasswordTooShort
	}
	return newHasher(1).Hash(ctx, password)
}

type Device struct {
	Name   string
	Client string
}

// SignIn checks a profile's password and starts a session for the device, answering the token
// the device sends from then on.
func (s *Service) SignIn(ctx context.Context, name, password string, device Device) (string, domain.Profile, error) {
	profile, hash, err := s.store.ProfileByName(ctx, name)
	if errors.Is(err, store.ErrNotFound) {
		hash, err = "", nil
	}
	if err != nil {
		return "", domain.Profile{}, err
	}
	match, stale, err := s.verify(ctx, hash, password)
	if err != nil {
		return "", domain.Profile{}, err
	}
	if !match {
		return "", domain.Profile{}, ErrInvalidCredentials
	}
	if stale {
		fresh, err := s.hasher.Hash(ctx, password)
		if err == nil {
			err = s.store.SetPasswordHash(ctx, profile.ID, fresh)
		}
		if err != nil {
			return "", domain.Profile{}, err
		}
	}
	token, err := s.startSession(ctx, profile, device, domain.SignInIdentity{})
	return token, profile, err
}

// SetUp adds the server's first profile, an admin, and signs in the device that set it up.
func (s *Service) SetUp(ctx context.Context, name, password string, device Device) (string, domain.Profile, error) {
	hash, err := HashPassword(ctx, password)
	if err != nil {
		return "", domain.Profile{}, err
	}
	profile, err := s.store.AddFirstAdmin(ctx, name, hash)
	if err != nil {
		return "", domain.Profile{}, err
	}
	token, err := s.startSession(ctx, profile, device, domain.SignInIdentity{})
	return token, profile, err
}

// verify is the hasher's Verify, refusing every password for a profile with none, or with no
// profile: it is verified against the dummy all the same, so neither answers sooner than a profile
// with a password.
func (s *Service) verify(ctx context.Context, hash, password string) (match, stale bool, err error) {
	if hash == "" {
		_, _, err := s.hasher.Verify(ctx, s.dummy, password)
		return false, false, err
	}
	return s.hasher.Verify(ctx, hash, password)
}

// SignInAs starts a session for a profile its account at a sign-in provider signed in, which the
// caller has checked; the session ends when the account is unlinked.
func (s *Service) SignInAs(ctx context.Context, profile domain.Profile, identity domain.SignInIdentity, device Device) (string, error) {
	return s.startSession(ctx, profile, device, identity)
}

func (s *Service) startSession(ctx context.Context, profile domain.Profile, device Device, identity domain.SignInIdentity) (string, error) {
	token, tokenHash := newToken()
	_, err := s.store.CreateSession(ctx, store.NewSession{
		Kind: domain.SessionDevice, ProfileID: profile.ID, TokenHash: tokenHash,
		DeviceName: device.Name, Client: device.Client, ExpiresAt: new(time.Now().Add(idleExpiry)),
		Identity: identity,
	})
	if err != nil {
		return "", err
	}
	return token, nil
}

// Authenticate finds the session a device token or API key belongs to.
func (s *Service) Authenticate(ctx context.Context, token string) (domain.Session, error) {
	if !strings.HasPrefix(token, tokenPrefix) {
		return domain.Session{}, ErrUnauthenticated
	}
	now := time.Now()
	session, lastSeen, err := s.store.SessionByToken(ctx, hashToken(token), now)
	if errors.Is(err, store.ErrNotFound) {
		return domain.Session{}, ErrUnauthenticated
	}
	if err != nil {
		return domain.Session{}, err
	}
	if now.Sub(lastSeen) > touchEvery {
		var expires *time.Time
		switch session.Kind {
		case domain.SessionDevice:
			expires = new(now.Add(idleExpiry))
		case domain.SessionKey:
		}
		if err := s.store.TouchSession(ctx, session.ID, now, expires); err != nil {
			return domain.Session{}, err
		}
	}
	return session, nil
}

func (s *Service) SignOut(ctx context.Context, session uuid.UUID) error {
	return s.store.DeleteSession(ctx, session)
}
