package webpush

import (
	"context"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func b64(t *testing.T, s string) []byte {
	t.Helper()
	b, err := base64.RawURLEncoding.DecodeString(strings.Join(strings.Fields(s), ""))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// A message is sealed exactly as RFC 8291's example in its section 5.
func TestAMessageIsSealedAsRFC8291Says(t *testing.T) {
	as, err := ecdh.P256().NewPrivateKey(b64(t, "yfWPiYE-n46HLnH0KqZOF1fJJU3MYrct3AELtAQ-oRw"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := seal([]byte("When I grow up, I want to be a watermelon"),
		b64(t, "BCVxsr7N_eNgVRqvHtD0zTZsEc6-VV-JvLexhqUzORcx aOzi6-AYWXvTBHm4bjyPjs7Vd8pZGH6SRpkNtoIAiw4"),
		b64(t, "BTBZMqHH6r4Tts7J_aSIgg"), as, b64(t, "DGv6ra1nlYgDCS1FRnbzlw"))
	if err != nil {
		t.Fatal(err)
	}
	want := `DGv6ra1nlYgDCS1FRnbzlwAAEABBBP4z9KsN6nGRTbVYI_c7VJSPQTBtkgcy27ml
		mlMoZIIgDll6e3vCYLocInmYWAmS6TlzAC8wEqKK6PBru3jl7A_yl95bQpu6cVPT
		pK4Mqgkf1CXztLVBSt2Ks3oZwbuwXPXLWyouBWLVWGNWQexSgSxsj_Qulcy4a-fN`
	if string(got) != string(b64(t, want)) {
		t.Errorf("sealed = %s\nwant %s", base64.RawURLEncoding.EncodeToString(got), strings.Join(strings.Fields(want), ""))
	}
}

func subscriber(t *testing.T) Subscription {
	t.Helper()
	k, err := ecdh.P256().GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	return Subscription{P256DH: k.PublicKey().Bytes(), Auth: make([]byte, 16)}
}

// A push service is handed the sealed message signed with the server's key, for its own origin,
// and a subscription it says has ended is ErrGone.
func TestAMessageIsSignedForItsPushService(t *testing.T) {
	key, err := NewKey()
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(key)
	if err != nil {
		t.Fatal(err)
	}
	var got *http.Request
	var body []byte
	gone := false
	svc := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, body = r, must(io.ReadAll(r.Body))
		if gone {
			w.WriteHeader(http.StatusGone)
			return
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer svc.Close()
	s.client = svc.Client()
	sub := subscriber(t)
	sub.Endpoint = svc.URL + "/push/abc"
	if err := s.Send(t.Context(), sub, Message{Payload: []byte(`{"title":"Severance"}`), Topic: "show", TTL: time.Hour}); err != nil {
		t.Fatal(err)
	}
	if got.Header.Get("Content-Encoding") != "aes128gcm" || got.Header.Get("TTL") != "3600" || got.Header.Get("Topic") != "show" {
		t.Errorf("headers = %v", got.Header)
	}
	if len(body) != headerSize+len(`{"title":"Severance"}`)+1+16 {
		t.Errorf("body is %d bytes", len(body))
	}
	auth := strings.TrimPrefix(got.Header.Get("Authorization"), "vapid ")
	token, k, _ := strings.Cut(auth, ", k=")
	token = strings.TrimPrefix(token, "t=")
	if string(b64(t, k)) != string(s.PublicKey()) {
		t.Errorf("k = %s, want the sender's key", k)
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("token = %q", token)
	}
	var claims struct {
		Aud string `json:"aud"`
		Exp int64  `json:"exp"`
		Sub string `json:"sub"`
	}
	if err := json.Unmarshal(b64(t, parts[1]), &claims); err != nil || claims.Aud != svc.URL || claims.Sub == "" ||
		claims.Exp <= time.Now().Unix() {
		t.Errorf("claims = %+v %v, want the push service's origin, a subject, and an expiry ahead", claims, err)
	}
	pub, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), s.PublicKey())
	if err != nil {
		t.Fatal(err)
	}
	sig, digest := b64(t, parts[2]), sha256.Sum256([]byte(parts[0]+"."+parts[1]))
	if !ecdsa.Verify(pub, digest[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])) {
		t.Error("the token's signature does not verify with the sender's key")
	}

	gone = true
	if err := s.Send(t.Context(), sub, Message{Payload: []byte("{}")}); !errors.Is(err, ErrGone) {
		t.Errorf("a subscription ended: %v, want ErrGone", err)
	}
	if err := s.Send(t.Context(), sub, Message{Payload: make([]byte, MaxPayload+1)}); !errors.Is(err, ErrTooLarge) {
		t.Errorf("too large a message: %v, want ErrTooLarge", err)
	}
}

// A browser gives its endpoint, so one that is plain HTTP, or at the server's own or its network's
// addresses, is never sent to.
func TestAnEndpointIsAnHTTPSAddressOnTheInternet(t *testing.T) {
	for _, endpoint := range []string{"http://push.example.com/x", "https://user:pw@push.example.com/x", "file:///etc/passwd", "https:///x"} {
		if err := Check(endpoint); !errors.Is(err, ErrNotPublic) {
			t.Errorf("Check(%q) = %v, want ErrNotPublic", endpoint, err)
		}
	}
	key, err := NewKey()
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(key)
	if err != nil {
		t.Fatal(err)
	}
	svc := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusCreated) }))
	defer svc.Close()
	sub := subscriber(t)
	sub.Endpoint = svc.URL + "/push"
	if err := s.Send(context.Background(), sub, Message{Payload: []byte("{}")}); !errors.Is(err, ErrNotPublic) {
		t.Errorf("an endpoint on loopback: %v, want ErrNotPublic", err)
	}
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}
