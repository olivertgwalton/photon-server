package kv

import (
	"context"
	"strconv"
	"time"

	"github.com/valkey-io/valkey-go"
)

func (k *KV) pauseKey(name string) string { return k.key("pause:" + name) }

// pause sets the pause to the longer of the one held and the one asked for, in one step, so a
// short answer from one node never cuts short a long one from another.
var pause = valkey.NewLuaScript(`
if redis.call('PTTL', KEYS[1]) < tonumber(ARGV[1]) then
  redis.call('SET', KEYS[1], 1, 'PX', ARGV[1])
end
return 0`)

// Pause holds back every node's requests to name for d, as the provider asked.
func (k *KV) Pause(ctx context.Context, name string, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	return pause.Exec(ctx, k.client, []string{k.pauseKey(name)}, []string{strconv.FormatInt(d.Milliseconds(), 10)}).Error()
}

// Paused answers how long requests to name are still held back, zero for none.
func (k *KV) Paused(ctx context.Context, name string) (time.Duration, error) {
	ms, err := k.client.Do(ctx, k.client.B().Pttl().Key(k.pauseKey(name)).Build()).AsInt64()
	return time.Duration(max(ms, 0)) * time.Millisecond, err
}
