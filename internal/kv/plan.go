package kv

import (
	"context"
	"time"
	"uuid"

	"github.com/valkey-io/valkey-go"
)

func (k *KV) planKey(session uuid.UUID) string { return k.key("plan:" + session.String()) }

// SavePlan keeps how a play session's HLS is to be made, as an API decided it, for ttl, so whichever
// node is asked for the session opens it as decided.
func (k *KV) SavePlan(ctx context.Context, session uuid.UUID, plan []byte, ttl time.Duration) error {
	return k.client.Do(ctx, k.client.B().Set().Key(k.planKey(session)).Value(valkey.BinaryString(plan)).Px(ttl).Build()).Error()
}

// Plan answers a play session's plan, ok false for one unknown or lapsed.
func (k *KV) Plan(ctx context.Context, session uuid.UUID) (plan []byte, ok bool, err error) {
	plan, err = k.client.Do(ctx, k.client.B().Get().Key(k.planKey(session)).Build()).AsBytes()
	if valkey.IsValkeyNil(err) {
		return nil, false, nil
	}
	return plan, err == nil, err
}
