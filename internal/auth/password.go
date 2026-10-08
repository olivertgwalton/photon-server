package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// argon2id with RFC 9106's second recommended parameters: 64 MiB, three passes.
const (
	argonTime    = 3
	argonMemory  = 64 << 10 // KiB
	argonThreads = 4
	argonKeyLen  = 32
	saltLen      = 16
)

var errBadHash = errors.New("not an argon2id hash")

// hasher bounds how many hashes run at once: each takes 64 MiB, and a flood of sign-ins must not
// exhaust a small server's memory.
type hasher struct {
	slots chan struct{}
}

func newHasher(concurrent int) *hasher {
	return &hasher{slots: make(chan struct{}, concurrent)}
}

func (h *hasher) key(ctx context.Context, password, salt []byte, t, m uint32, p uint8) ([]byte, error) {
	select {
	case h.slots <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-h.slots }()
	return argon2.IDKey(password, salt, t, m, p, argonKeyLen), nil
}

// Hash encodes a password as a PHC string, which carries its own parameters so they can be raised
// later without invalidating stored hashes.
func (h *hasher) Hash(ctx context.Context, password string) (string, error) {
	salt := make([]byte, saltLen)
	rand.Read(salt)
	key, err := h.key(ctx, []byte(password), salt, argonTime, argonMemory, argonThreads)
	if err != nil {
		return "", err
	}
	b64 := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads, b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

// Verify reports whether password matches the hash, and whether the hash was made with weaker
// parameters than today's and should be replaced.
func (h *hasher) Verify(ctx context.Context, encoded, password string) (match, stale bool, err error) {
	fields := strings.Split(encoded, "$")
	if len(fields) != 6 || fields[1] != "argon2id" {
		return false, false, errBadHash
	}
	var version int
	var m, t uint32
	var p uint8
	if _, err := fmt.Sscanf(fields[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, false, errBadHash
	}
	if _, err := fmt.Sscanf(fields[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return false, false, errBadHash
	}
	b64 := base64.RawStdEncoding
	salt, err1 := b64.DecodeString(fields[4])
	want, err2 := b64.DecodeString(fields[5])
	if err1 != nil || err2 != nil {
		return false, false, errBadHash
	}
	got, err := h.key(ctx, []byte(password), salt, t, m, p)
	if err != nil {
		return false, false, err
	}
	return subtle.ConstantTimeCompare(got, want) == 1, m < argonMemory || t < argonTime, nil
}
