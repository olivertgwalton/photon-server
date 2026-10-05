package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
)

// tokenPrefix marks a photon-server token, so secret scanners can find one that leaked.
const tokenPrefix = "pst_"

// newToken mints a device token: 256 random bits. Only its hash is stored.
func newToken() (token string, hash []byte) {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	token = tokenPrefix + base64.RawURLEncoding.EncodeToString(b)
	return token, hashToken(token)
}

// hashToken is SHA-256: a token has 256 bits of entropy, so a slow hash would protect nothing.
func hashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}
