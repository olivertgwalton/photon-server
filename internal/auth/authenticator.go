package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec // RFC 6238's HMAC-SHA1, the one every authenticator app makes codes by
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/store"
)

// An authenticator app's code is RFC 6238's: six digits from HMAC-SHA1 over the 30-second steps
// since the epoch.
const (
	codePeriod = 30 * time.Second
	// codeSkew takes a code for the step either side of now's too, for a phone's clock a little out.
	codeSkew = 1
	// A secret is RFC 4226's recommended 160 bits or more, and no longer than a SHA-1 block.
	minSecret = 20
	maxSecret = 64
)

var (
	ErrCodeRequired = errors.New("the profile's authenticator app is asked for a code")
	ErrWrongCode    = errors.New("that code isn't the authenticator app's, or was used already")
	ErrBadSecret    = errors.New("secret is the authenticator's key in base32, of 160 to 512 bits")
)

var secretEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

func totp(secret []byte, step int64) string {
	mac := hmac.New(sha1.New, secret)
	mac.Write(binary.BigEndian.AppendUint64(nil, uint64(step)))
	sum := mac.Sum(nil)
	at := sum[len(sum)-1] & 0x0f
	return fmt.Sprintf("%06d", (binary.BigEndian.Uint32(sum[at:])&0x7fffffff)%1_000_000)
}

// codeStep is the step a code was made for, within codeSkew of now's.
func codeStep(secret []byte, typed string, now time.Time) (int64, bool) {
	typed = strings.ReplaceAll(typed, " ", "")
	step := now.Unix() / int64(codePeriod.Seconds())
	for at := step - codeSkew; at <= step+codeSkew; at++ {
		if subtle.ConstantTimeCompare([]byte(totp(secret, at)), []byte(typed)) == 1 {
			return at, true
		}
	}
	return 0, false
}

// SetAuthenticator gives a profile the authenticator app whose key is secret, once code shows the
// app holds it; that code is then used.
func (s *Service) SetAuthenticator(ctx context.Context, profile uuid.UUID, secret, typed string) error {
	key, err := secretEncoding.DecodeString(strings.TrimRight(strings.ToUpper(strings.ReplaceAll(secret, " ", "")), "="))
	if err != nil || len(key) < minSecret || len(key) > maxSecret {
		return ErrBadSecret
	}
	step, ok := codeStep(key, typed, time.Now())
	if !ok {
		return ErrWrongCode
	}
	return s.store.SetAuthenticator(ctx, profile, key, step)
}

// Authenticator is when a profile was given its authenticator, of those a manager by keeps when by
// is set.
func (s *Service) Authenticator(ctx context.Context, profile uuid.UUID, by *uuid.UUID) (time.Time, error) {
	a, err := s.store.Authenticator(ctx, profile, by)
	return a.AddedAt, err
}

func (s *Service) RemoveAuthenticator(ctx context.Context, profile uuid.UUID, by *uuid.UUID) error {
	return s.store.RemoveAuthenticator(ctx, profile, by)
}

// secondFactor takes a code from the profile's authenticator, where it has one, once.
func (s *Service) secondFactor(ctx context.Context, profile uuid.UUID, typed string) error {
	a, err := s.store.Authenticator(ctx, profile, nil)
	switch {
	case errors.Is(err, store.ErrNotFound):
		return nil
	case err != nil:
		return err
	case typed == "":
		return ErrCodeRequired
	}
	step, ok := codeStep(a.Secret, typed, time.Now())
	if !ok {
		return ErrWrongCode
	}
	err = s.store.TakeAuthenticatorStep(ctx, profile, step)
	if errors.Is(err, store.ErrNotFound) {
		return ErrWrongCode
	}
	return err
}
