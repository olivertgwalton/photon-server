package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/kv"
)

// Pairing follows RFC 8628: a television shows a short code, a signed-in phone approves it, and
// the television, polling, receives its own token.
const (
	pairingTTL   = 10 * time.Minute
	PollInterval = 5 * time.Second
	// userCodeAlphabet has no vowels, so a code never spells a word, and no characters easily
	// confused when read off a screen. Eight of them are 34.5 bits.
	userCodeAlphabet = "BCDFGHJKLMNPQRSTVWXZ"
	userCodeLen      = 8
)

var (
	ErrPairingNotFound = errors.New("no pairing is waiting for that code")
	errNoFreeCode      = errors.New("no free pairing code")
)

type PairingStart struct {
	DeviceCode string
	UserCode   string
	ExpiresIn  time.Duration
}

func (s *Service) StartPairing(ctx context.Context, d Device) (PairingStart, error) {
	for range 5 {
		code := newUserCode()
		secret := make([]byte, 32)
		_, _ = rand.Read(secret)
		deviceSecret := base64.RawURLEncoding.EncodeToString(secret)
		err := s.kv.StartPairing(ctx, code, hashToken(deviceSecret), kv.Pairing{Device: d.Name, Client: d.Client}, pairingTTL)
		if errors.Is(err, kv.ErrUserCodeTaken) {
			continue
		}
		if err != nil {
			return PairingStart{}, err
		}
		return PairingStart{DeviceCode: code + "." + deviceSecret, UserCode: code[:4] + "-" + code[4:], ExpiresIn: pairingTTL}, nil
	}
	return PairingStart{}, errNoFreeCode
}

func newUserCode() string {
	b := make([]byte, userCodeLen)
	for i := range b {
		b[i] = userCodeAlphabet[randIndex(len(userCodeAlphabet))]
	}
	return string(b)
}

// randIndex draws uniformly from [0, n) by rejecting the bytes past the largest multiple of n.
func randIndex(n int) int {
	limit := 256 - 256%n
	var b [1]byte
	for {
		_, _ = rand.Read(b[:])
		if int(b[0]) < limit {
			return int(b[0]) % n
		}
	}
}

// ApprovePairing gives the waiting television to the approving session's profile.
func (s *Service) ApprovePairing(ctx context.Context, approver domain.Session, userCode string) (Device, error) {
	code := strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(userCode))
	p, ok, err := s.kv.ApprovePairing(ctx, code, approver.Profile.ID)
	if err != nil {
		return Device{}, err
	}
	if !ok {
		return Device{}, ErrPairingNotFound
	}
	return Device{Name: p.Device, Client: p.Client}, nil
}

// PollPairing answers a television asking after its code. Once approved it receives a device
// token of its own, exactly once.
func (s *Service) PollPairing(ctx context.Context, deviceCode string) (kv.PairingState, string, domain.Profile, error) {
	code, secret, ok := strings.Cut(deviceCode, ".")
	if !ok {
		return kv.PairingExpired, "", domain.Profile{}, nil
	}
	state, p, err := s.kv.PollPairing(ctx, code, hashToken(secret), PollInterval)
	if err != nil || state != kv.PairingApproved {
		return state, "", domain.Profile{}, err
	}
	profile, err := s.store.ProfileByID(ctx, p.Profile)
	if err != nil {
		return state, "", domain.Profile{}, err
	}
	token, err := s.startSession(ctx, profile, Device{Name: p.Device, Client: p.Client})
	return state, token, profile, err
}
