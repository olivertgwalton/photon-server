// Package playback decides how a title plays and hands out the addresses it plays from.
package playback

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"net/url"
	"strconv"
	"time"
)

// Signer makes stream addresses a player can fetch without a token: a path signed until an
// expiry. AVPlayer and most players fetch a stream, its playlists and its segments with no
// headers of their own, and a token in an address ends up in logs; a signature that names one
// path and lapses keeps neither problem.
type Signer struct{ key []byte }

func NewSigner(key []byte) Signer { return Signer{key: key} }

// Sign answers path with its expiry and signature as query parameters.
func (s Signer) Sign(path string, until time.Time) string {
	exp := strconv.FormatInt(until.Unix(), 10)
	return path + "?" + url.Values{"exp": {exp}, "sig": {s.mac(path, exp)}}.Encode()
}

// Valid reports whether sig signs path until exp, and exp is still to come.
func (s Signer) Valid(path, exp, sig string, now time.Time) bool {
	until, err := strconv.ParseInt(exp, 10, 64)
	if err != nil || now.Unix() > until {
		return false
	}
	return hmac.Equal([]byte(sig), []byte(s.mac(path, exp)))
}

func (s Signer) mac(path, exp string) string {
	m := hmac.New(sha256.New, s.key)
	m.Write([]byte(path + "\n" + exp))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}
