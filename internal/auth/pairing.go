package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"math/big"
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
)

// CodeStyle is how a pairing's user code is written: photon's letters, or the six digits Jellyfin's
// apps take and nothing else. Either is approved wherever a code is entered.
type CodeStyle string

const (
	CodeLetters CodeStyle = "letters"
	CodeDigits  CodeStyle = "digits"
)

// form is a style's alphabet and length. The letters have no vowels, so a code never spells a
// word, and no characters easily confused when read off a screen: eight of them are 34.5 bits.
// Six digits are 19.9 bits, which the approvals each profile may make keep out of reach.
func (c CodeStyle) form() (alphabet string, length int) {
	switch c {
	case CodeLetters:
		return "BCDFGHJKLMNPQRSTVWXZ", 8
	case CodeDigits:
		return "0123456789", 6
	}
	return "", 0
}

// shown is a user code as a television shows it: letters as XXXX-XXXX, digits as they are.
func (c CodeStyle) shown(code string) string {
	switch c {
	case CodeLetters:
		return code[:4] + "-" + code[4:]
	case CodeDigits:
	}
	return code
}

var (
	ErrPairingNotFound = errors.New("no pairing is waiting for that code")
	errNoFreeCode      = errors.New("no free pairing code")
)

type PairingStart struct {
	DeviceCode string
	UserCode   string
	ExpiresIn  time.Duration
}

func (s *Service) StartPairing(ctx context.Context, d Device, style CodeStyle) (PairingStart, error) {
	for range 5 {
		code, err := style.newCode()
		if err != nil {
			return PairingStart{}, err
		}
		secret := make([]byte, 32)
		rand.Read(secret)
		deviceSecret := base64.RawURLEncoding.EncodeToString(secret)
		err = s.kv.StartPairing(ctx, code, hashToken(deviceSecret), kv.Pairing{Device: d.Name, Client: d.Client, Style: string(style)}, pairingTTL)
		if errors.Is(err, kv.ErrUserCodeTaken) {
			continue
		}
		if err != nil {
			return PairingStart{}, err
		}
		return PairingStart{DeviceCode: code + "." + deviceSecret, UserCode: style.shown(code), ExpiresIn: pairingTTL}, nil
	}
	return PairingStart{}, errNoFreeCode
}

// newCode draws each character uniformly from crypto/rand, so a digit code may lead with zeros.
func (c CodeStyle) newCode() (string, error) {
	alphabet, length := c.form()
	b := make([]byte, length)
	size := big.NewInt(int64(len(alphabet)))
	for i := range b {
		n, err := rand.Int(rand.Reader, size)
		if err != nil {
			return "", err
		}
		b[i] = alphabet[n.Int64()]
	}
	return string(b), nil
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

// A Pairing is what waits on a code: the television that asked, the code it shows, and when.
type Pairing struct {
	Device   Device
	UserCode string
	Started  time.Time
}

// PairingStatus answers a television asking whether its code is approved yet, without handing out
// its token.
func (s *Service) PairingStatus(ctx context.Context, deviceCode string) (kv.PairingState, Pairing, error) {
	code, secret, ok := strings.Cut(deviceCode, ".")
	if !ok {
		return kv.PairingExpired, Pairing{}, nil
	}
	state, p, remaining, err := s.kv.PairingStatus(ctx, code, hashToken(secret))
	if err != nil || state == kv.PairingExpired {
		return state, Pairing{}, err
	}
	return state, Pairing{
		Device: Device{Name: p.Device, Client: p.Client}, UserCode: CodeStyle(p.Style).shown(code), Started: time.Now().Add(remaining - pairingTTL),
	}, nil
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
