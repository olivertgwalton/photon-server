package kv

import (
	"context"
	"strconv"
	"time"

	"github.com/valkey-io/valkey-go"
)

// Limit allows Burst requests at once, then one every Every.
type Limit struct {
	Every time.Duration
	Burst int
}

// gcra is the generic cell rate algorithm: one stored time per key, the theoretical arrival time
// of the next request, read and moved in one step. The clock is Valkey's, so every node agrees.
var gcra = valkey.NewLuaScript(`
local t = redis.call('TIME')
local now = tonumber(t[1]) * 1000000 + tonumber(t[2])
local every = tonumber(ARGV[1])
local window = every * tonumber(ARGV[2])
local tat = math.max(tonumber(redis.call('GET', KEYS[1]) or now), now)
local next = tat + every
if next - now > window then
  return next - window - now
end
redis.call('SET', KEYS[1], next, 'PX', math.ceil((next - now) / 1000))
return 0`)

// Allow takes one request from key's allowance. When refused, it answers how long until the next
// is allowed. The limit is the same on every node, as the state is in Valkey.
func (k *KV) Allow(ctx context.Context, key string, l Limit) (time.Duration, error) {
	wait, err := gcra.Exec(ctx, k.client, []string{"photon:limit:" + key}, []string{
		strconv.FormatInt(l.Every.Microseconds(), 10), strconv.Itoa(l.Burst),
	}).AsInt64()
	return time.Duration(wait) * time.Microsecond, err
}

type limiter interface {
	Allow(ctx context.Context, key string, l Limit) (time.Duration, error)
}

// Wait blocks until key's allowance lets one more request through.
func Wait(ctx context.Context, l limiter, key string, limit Limit) error {
	for {
		wait, err := l.Allow(ctx, key, limit)
		if err != nil || wait == 0 {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
}
