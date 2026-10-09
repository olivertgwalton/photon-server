package kv

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"time"
	"uuid"

	"github.com/valkey-io/valkey-go"
)

// SignInFlow is a browser sent to a sign-in provider, kept until the provider sends it back: what
// the server checks the provider's answer against, and what it then does.
type SignInFlow struct {
	Provider string `json:"provider"`
	// Issuer is the provider's when the browser left, so an answer from any other is refused.
	Issuer string `json:"issuer"`
	// Verifier is the PKCE code verifier (RFC 7636), and Nonce what the ID token must carry.
	Verifier string `json:"verifier"`
	Nonce    string `json:"nonce"`
	// Linking is the profile linking its account at the provider; zero for a sign-in.
	Linking uuid.UUID `json:"linking,omitzero"`
	// Device and Client are what a sign-in names its session.
	Device string `json:"device,omitzero"`
	Client string `json:"client,omitzero"`
	// To is the path on this server the browser goes on to.
	To string `json:"to"`
}

// A flow is a key named by its state's hash, never the state.
func (k *KV) signInKey(stateHash []byte) string {
	return k.key("signin:" + hex.EncodeToString(stateHash))
}

// StartSignIn keeps a flow for ttl, answering false where its state is in use.
func (k *KV) StartSignIn(ctx context.Context, stateHash []byte, f SignInFlow, ttl time.Duration) (bool, error) {
	b, err := json.Marshal(f)
	if err != nil {
		return false, err
	}
	err = k.client.Do(ctx, k.client.B().Set().Key(k.signInKey(stateHash)).Value(string(b)).Nx().Px(ttl).Build()).Error()
	if valkey.IsValkeyNil(err) {
		return false, nil
	}
	return err == nil, err
}

// TakeSignIn answers a flow and forgets it, so a provider's answer is taken once. ok is false for
// a state unknown, expired or used.
func (k *KV) TakeSignIn(ctx context.Context, stateHash []byte) (f SignInFlow, ok bool, err error) {
	b, err := k.client.Do(ctx, k.client.B().Getdel().Key(k.signInKey(stateHash)).Build()).AsBytes()
	if valkey.IsValkeyNil(err) {
		return SignInFlow{}, false, nil
	}
	if err != nil {
		return SignInFlow{}, false, err
	}
	return f, true, json.Unmarshal(b, &f)
}
