// Package nodecall signs the requests one node makes of another, and checks them, so a node does
// for the cluster only what another node asked of it.
package nodecall

import (
	"bytes"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	// header carries a request's signature and when it stops being honoured.
	header = "Photon-Node-Signature"
	// within is how long a signed request has to arrive: long enough for a clock a little off.
	within = 30 * time.Second
	// maxBody bounds what a node is asked to read before the signature is checked.
	maxBody = 1 << 20
)

// errUnsigned is a request no node signed, or signed for something else.
var errUnsigned = errors.New("the request is not signed by a node of this cluster")

// Key signs and checks the requests nodes make of each other.
type Key struct{ key []byte }

// NewKey derives the key nodes sign their requests with from the cluster's signing key, under a
// label of its own, so no signature handed to a client is ever one a node honours.
func NewKey(signingKey []byte) (Key, error) {
	k, err := hkdf.Key(sha256.New, signingKey, nil, "photon node call", sha256.Size)
	return Key{key: k}, err
}

// Sign signs r, which carries body, for within from now.
func (k Key) Sign(r *http.Request, body []byte) { k.signUntil(r, body, time.Now().Add(within)) }

func (k Key) signUntil(r *http.Request, body []byte, until time.Time) {
	exp := strconv.FormatInt(until.Unix(), 10)
	r.Header.Set(header, exp+" "+k.mac(r.Method, r.URL.Path, exp, body))
}

func (k Key) mac(method, path, exp string, body []byte) string {
	sum := sha256.Sum256(body)
	h := hmac.New(sha256.New, k.key)
	_, _ = fmt.Fprintf(h, "%s\n%s\n%s\n%x", method, path, exp, sum)
	return hex.EncodeToString(h.Sum(nil))
}

// Verify passes on to next only a request another node signed, unchanged and in time; any other
// is answered 401.
func (k Key) Verify(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := k.check(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		next.ServeHTTP(w, r)
	})
}

func (k Key) check(r *http.Request) ([]byte, error) {
	exp, sig, ok := strings.Cut(r.Header.Get(header), " ")
	if !ok {
		return nil, errUnsigned
	}
	unix, err := strconv.ParseInt(exp, 10, 64)
	if err != nil || time.Now().After(time.Unix(unix, 0)) {
		return nil, errUnsigned
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBody))
	if err != nil {
		return nil, err
	}
	if !hmac.Equal([]byte(sig), []byte(k.mac(r.Method, r.URL.Path, exp, body))) {
		return nil, errUnsigned
	}
	return body, nil
}
