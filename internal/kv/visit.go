package kv

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"time"
	"uuid"

	"github.com/valkey-io/valkey-go"
)

// Visit is a profile's visit to a plugin's page, which the plugin claims by its code once.
type Visit struct {
	Plugin  string    `json:"plugin"`
	Page    string    `json:"page"`
	Profile uuid.UUID `json:"profile"`
}

// A visit is a key named by its code's hash, never the code.
func (k *KV) visitKey(codeHash []byte) string { return k.key("visit:" + hex.EncodeToString(codeHash)) }

// StartVisit keeps a visit for ttl.
func (k *KV) StartVisit(ctx context.Context, codeHash []byte, v Visit, ttl time.Duration) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return k.client.Do(ctx, k.client.B().Set().Key(k.visitKey(codeHash)).Value(string(b)).Px(ttl).Build()).Error()
}

// TakeVisit answers a visit and forgets it, so a code is claimed once. ok is false for a code
// unknown, expired or claimed.
func (k *KV) TakeVisit(ctx context.Context, codeHash []byte) (v Visit, ok bool, err error) {
	s, err := k.client.Do(ctx, k.client.B().Getdel().Key(k.visitKey(codeHash)).Build()).ToString()
	if valkey.IsValkeyNil(err) {
		return v, false, nil
	}
	if err != nil {
		return v, false, err
	}
	return v, true, json.Unmarshal([]byte(s), &v)
}
