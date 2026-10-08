package kv

import (
	"context"
	"encoding/json"
	"time"

	"github.com/valkey-io/valkey-go"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// The restore under way, which every node stops for, lapsing unless the node restoring keeps it;
// and how the last ended, kept for good.
const (
	restoreKey  = "restore"
	restoredKey = "restored"
)

// BeginRestore keeps r as the restore under way for ttl, answering false where one already is.
func (k *KV) BeginRestore(ctx context.Context, r domain.Restore, ttl time.Duration) (bool, error) {
	v, err := json.Marshal(r)
	if err != nil {
		return false, err
	}
	err = k.client.Do(ctx, k.client.B().Set().Key(k.key(restoreKey)).Value(string(v)).Nx().Px(ttl).Build()).Error()
	if valkey.IsValkeyNil(err) {
		return false, nil
	}
	return err == nil, err
}

// SaveRestore replaces the restore under way, for ttl, and only while there is one.
func (k *KV) SaveRestore(ctx context.Context, r domain.Restore, ttl time.Duration) error {
	v, err := json.Marshal(r)
	if err != nil {
		return err
	}
	err = k.client.Do(ctx, k.client.B().Set().Key(k.key(restoreKey)).Value(string(v)).Xx().Px(ttl).Build()).Error()
	if valkey.IsValkeyNil(err) {
		return nil
	}
	return err
}

// KeepRestore keeps the restore under way for ttl from now; one cleared stays cleared.
func (k *KV) KeepRestore(ctx context.Context, ttl time.Duration) error {
	return k.client.Do(ctx, k.client.B().Pexpire().Key(k.key(restoreKey)).Milliseconds(ttl.Milliseconds()).Build()).Error()
}

// Restoring answers the restore under way, or false for none.
func (k *KV) Restoring(ctx context.Context) (domain.Restore, bool, error) {
	return read[domain.Restore](ctx, k, restoreKey)
}

func (k *KV) EndRestore(ctx context.Context) error {
	return k.client.Do(ctx, k.client.B().Del().Key(k.key(restoreKey)).Build()).Error()
}

func (k *KV) SaveRestoreOutcome(ctx context.Context, o domain.RestoreOutcome) error {
	v, err := json.Marshal(o)
	if err != nil {
		return err
	}
	return k.client.Do(ctx, k.client.B().Set().Key(k.key(restoredKey)).Value(string(v)).Build()).Error()
}

// LastRestore answers how the last restore ended, or false for none since the server's keys
// were last cleared.
func (k *KV) LastRestore(ctx context.Context) (domain.RestoreOutcome, bool, error) {
	return read[domain.RestoreOutcome](ctx, k, restoredKey)
}

func read[T any](ctx context.Context, k *KV, name string) (T, bool, error) {
	var v T
	b, err := k.client.Do(ctx, k.client.B().Get().Key(k.key(name)).Build()).AsBytes()
	if valkey.IsValkeyNil(err) {
		return v, false, nil
	}
	if err != nil {
		return v, false, err
	}
	return v, true, json.Unmarshal(b, &v)
}
