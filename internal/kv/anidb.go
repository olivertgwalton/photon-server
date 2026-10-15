package kv

import (
	"context"
	"time"

	"github.com/valkey-io/valkey-go"
)

func (k *KV) aniDBKey(name string) string { return k.key("anidb:" + name) }

// KeepAniDB keeps one of AniDB's answers for ttl, every node reading it rather than asking again,
// as AniDB bans a client that asks for the same anime more than once a day.
func (k *KV) KeepAniDB(ctx context.Context, name string, answer []byte, ttl time.Duration) error {
	return k.client.Do(ctx, k.client.B().Set().Key(k.aniDBKey(name)).Value(valkey.BinaryString(answer)).Px(ttl).Build()).Error()
}

// AniDB answers one of AniDB's answers kept, ok false for one never kept or lapsed.
func (k *KV) AniDB(ctx context.Context, name string) (answer []byte, ok bool, err error) {
	answer, err = k.client.Do(ctx, k.client.B().Get().Key(k.aniDBKey(name)).Build()).AsBytes()
	if valkey.IsValkeyNil(err) {
		return nil, false, nil
	}
	return answer, err == nil, err
}
