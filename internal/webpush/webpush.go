// Package webpush sends messages to browsers through their push services: each sealed for its
// browser alone (RFC 8291), and signed with the server's VAPID key so a push service knows who
// sends it (RFC 8292).
package webpush

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"syscall"
	"time"

	"github.com/olivertgwalton/photon-server/internal/media"
)

const (
	// recordSize is the one record a message is sealed in. A push service takes a body of at most
	// 4096 bytes, and a message is far less.
	recordSize = 4096
	// MaxPayload is the most a message may carry: a record less its header, tag and delimiter.
	MaxPayload = recordSize - headerSize - aes.BlockSize - 1
	headerSize = 16 + 4 + 1 + 65
	// tokenLife is how long a VAPID token is good for; RFC 8292 allows at most a day.
	tokenLife = 12 * time.Hour
	// sendTimeout is how long a push service has to take a message.
	sendTimeout = 10 * time.Second
	// subject is who a push service may contact about the messages sent, as RFC 8292 asks of a
	// sender; Apple refuses a message without one.
	subject = "https://github.com/olivertgwalton/photon-server"
)

var (
	// ErrGone is a subscription its browser has ended: it is to be forgotten.
	ErrGone = errors.New("the subscription has ended")
	// ErrNotPublic is an endpoint that is not on the internet at large.
	ErrNotPublic = errors.New("a push endpoint is an https address on the internet")
	// ErrTooLarge is a message more than MaxPayload.
	ErrTooLarge = fmt.Errorf("a push message is at most %d bytes", MaxPayload)
)

// Subscription is where a browser is reached, and the keys it opens its messages with.
type Subscription struct {
	Endpoint string
	// P256DH is the browser's public key, uncompressed; Auth its 16-byte secret.
	P256DH []byte
	Auth   []byte
}

// Message is what is sent to a browser.
type Message struct {
	Payload []byte
	// Topic stands for what the message is of: a push service holding one undelivered replaces it
	// with the next of the same topic. At most 32 base64url characters.
	Topic string
	// TTL is how long a push service keeps it for a browser that is away.
	TTL time.Duration
}

// Sender sends messages signed with the server's VAPID key.
type Sender struct {
	key    *ecdsa.PrivateKey
	public []byte
	client *http.Client
}

// NewKey makes a VAPID key, as its raw P-256 scalar.
func NewKey() ([]byte, error) {
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	return k.Bytes()
}

// New is a sender signing with a key NewKey made.
func New(key []byte) (*Sender, error) {
	k, err := ecdsa.ParseRawPrivateKey(elliptic.P256(), key)
	if err != nil {
		return nil, err
	}
	public, err := k.PublicKey.Bytes()
	if err != nil {
		return nil, err
	}
	return &Sender{key: k, public: public, client: publicClient()}, nil
}

// PublicKey is the VAPID key a browser subscribes with, its applicationServerKey: uncompressed.
func (s *Sender) PublicKey() []byte { return s.public }

// Check refuses an endpoint that is not https, so none is sent in the clear; one that resolves to
// no public address is refused as it is dialled.
func Check(endpoint string) error {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return ErrNotPublic
	}
	return nil
}

// Send seals a message for a subscription and hands it to its push service. ErrGone for a
// subscription the browser has ended.
func (s *Sender) Send(ctx context.Context, sub Subscription, m Message) error {
	if len(m.Payload) > MaxPayload {
		return ErrTooLarge
	}
	if err := Check(sub.Endpoint); err != nil {
		return err
	}
	ephemeral, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	salt := make([]byte, 16)
	rand.Read(salt)
	body, err := seal(m.Payload, sub.P256DH, sub.Auth, ephemeral, salt)
	if err != nil {
		return err
	}
	token, err := s.token(sub.Endpoint, time.Now().Add(tokenLife))
	if err != nil {
		return err
	}
	sending, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(sending, http.MethodPost, sub.Endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Encoding", "aes128gcm")
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("TTL", strconv.Itoa(int(m.TTL.Seconds())))
	req.Header.Set("Authorization", "vapid t="+token+", k="+base64.RawURLEncoding.EncodeToString(s.public))
	if m.Topic != "" {
		req.Header.Set("Topic", m.Topic)
	}
	res, err := s.client.Do(req)
	if err != nil {
		// The endpoint names the browser to its push service, so the error names only its host.
		if ue, ok := errors.AsType[*url.Error](err); ok {
			err = ue.Err
		}
		return fmt.Errorf("%s: %w", req.URL.Host, err)
	}
	res.Body.Close()
	switch {
	case res.StatusCode == http.StatusNotFound || res.StatusCode == http.StatusGone:
		return ErrGone
	case res.StatusCode/100 != 2:
		return fmt.Errorf("%s answered %s", req.URL.Host, res.Status)
	}
	return nil
}

// seal encrypts a payload for a browser in one aes128gcm record, as RFC 8291 sets out: a key agreed
// between an ephemeral key and the browser's, mixed with its secret, and a salt.
func seal(payload, uaPublic, authSecret []byte, as *ecdh.PrivateKey, salt []byte) ([]byte, error) {
	ua, err := ecdh.P256().NewPublicKey(uaPublic)
	if err != nil {
		return nil, err
	}
	shared, err := as.ECDH(ua)
	if err != nil {
		return nil, err
	}
	asPublic := as.PublicKey().Bytes()
	ikm, err := hkdf.Key(sha256.New, shared, authSecret, "WebPush: info\x00"+string(uaPublic)+string(asPublic), 32)
	if err != nil {
		return nil, err
	}
	prk, err := hkdf.Extract(sha256.New, ikm, salt)
	if err != nil {
		return nil, err
	}
	cek, err := hkdf.Expand(sha256.New, prk, "Content-Encoding: aes128gcm\x00", 16)
	if err != nil {
		return nil, err
	}
	nonce, err := hkdf.Expand(sha256.New, prk, "Content-Encoding: nonce\x00", 12)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(cek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, headerSize+len(payload)+1+gcm.Overhead())
	out = append(out, salt...)
	out = binary.BigEndian.AppendUint32(out, recordSize)
	out = append(out, byte(len(asPublic)))
	out = append(out, asPublic...)
	// The last record ends its plaintext with 2.
	return gcm.Seal(out, nonce, slices.Concat(payload, []byte{2}), nil), nil
}

// token is a VAPID JWT for the push service an endpoint is at, signed ES256.
func (s *Sender) token(endpoint string, exp time.Time) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}
	claims, err := json.Marshal(struct {
		Aud string `json:"aud"`
		Exp int64  `json:"exp"`
		Sub string `json:"sub"`
	}{u.Scheme + "://" + u.Host, exp.Unix(), subject})
	if err != nil {
		return "", err
	}
	b64 := base64.RawURLEncoding
	signed := b64.EncodeToString([]byte(`{"typ":"JWT","alg":"ES256"}`)) + "." + b64.EncodeToString(claims)
	digest := sha256.Sum256([]byte(signed))
	r, sv, err := ecdsa.Sign(rand.Reader, s.key, digest[:])
	if err != nil {
		return "", err
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	sv.FillBytes(sig[32:])
	return signed + "." + b64.EncodeToString(sig), nil
}

// publicClient dials only addresses on the internet at large, checked as each is dialled so a name
// that resolves elsewhere later is refused too: an endpoint is a browser's to give.
func publicClient() *http.Client {
	dialer := &net.Dialer{
		Timeout: sendTimeout,
		ControlContext: func(_ context.Context, _, address string, _ syscall.RawConn) error {
			ap, err := netip.ParseAddrPort(address)
			if err != nil || !media.Public(ap.Addr()) {
				return ErrNotPublic
			}
			return nil
		},
	}
	return &http.Client{
		Transport: &http.Transport{DialContext: dialer.DialContext, ForceAttemptHTTP2: true, TLSHandshakeTimeout: sendTimeout},
		// A redirect would carry the token elsewhere.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}
