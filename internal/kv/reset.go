package kv

import (
	"context"
	"encoding/hex"
	"time"
	"uuid"

	"github.com/valkey-io/valkey-go"
)

// A password reset is a key named by its code's hash, never the code, holding the profile it resets.
func (k *KV) resetKey(codeHash []byte) string { return k.key("reset:" + hex.EncodeToString(codeHash)) }

// StartReset keeps a reset of profile for ttl, answering false where its code is in use.
func (k *KV) StartReset(ctx context.Context, codeHash []byte, profile uuid.UUID, ttl time.Duration) (bool, error) {
	err := k.client.Do(ctx, k.client.B().Set().Key(k.resetKey(codeHash)).Value(profile.String()).Nx().Px(ttl).Build()).Error()
	if valkey.IsValkeyNil(err) {
		return false, nil
	}
	return err == nil, err
}

// TakeReset answers the profile a reset is for and forgets it, so a code is used once. ok is false
// for a code unknown, expired or used.
func (k *KV) TakeReset(ctx context.Context, codeHash []byte) (profile uuid.UUID, ok bool, err error) {
	s, err := k.client.Do(ctx, k.client.B().Getdel().Key(k.resetKey(codeHash)).Build()).ToString()
	if valkey.IsValkeyNil(err) {
		return uuid.UUID{}, false, nil
	}
	if err != nil {
		return uuid.UUID{}, false, err
	}
	profile, err = uuid.Parse(s)
	return profile, err == nil, err
}
