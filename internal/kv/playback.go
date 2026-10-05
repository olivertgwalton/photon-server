package kv

import (
	"context"
	"strconv"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

func playbackKey(id uuid.UUID) string { return "photon:play:" + id.String() }

// SavePlayback writes a playback session, which lapses after ttl unless written again.
func (k *KV) SavePlayback(ctx context.Context, p domain.Playback, ttl time.Duration) error {
	key := playbackKey(p.ID)
	cmds := k.client.B()
	for _, r := range k.client.DoMulti(ctx,
		cmds.Hset().Key(key).FieldValue().
			FieldValue("profile", p.Profile.String()).FieldValue("item", p.Item.String()).
			FieldValue("version", p.Version.String()).FieldValue("method", string(p.Method)).
			FieldValue("state", string(p.State)).FieldValue("position_ms", strconv.FormatInt(p.Position.Milliseconds(), 10)).
			FieldValue("started", strconv.FormatInt(p.Started.Unix(), 10)).
			FieldValue("updated", strconv.FormatInt(p.Updated.Unix(), 10)).Build(),
		cmds.Expire().Key(key).Seconds(int64(ttl.Seconds())).Build(),
	) {
		if err := r.Error(); err != nil {
			return err
		}
	}
	return nil
}

// Playback answers a playback session, or false for one that has ended or lapsed.
func (k *KV) Playback(ctx context.Context, id uuid.UUID) (domain.Playback, bool, error) {
	m, err := k.client.Do(ctx, k.client.B().Hgetall().Key(playbackKey(id)).Build()).AsStrMap()
	if err != nil || len(m) == 0 {
		return domain.Playback{}, false, err
	}
	p := domain.Playback{ID: id, Method: domain.PlayMethod(m["method"]), State: domain.PlayState(m["state"])}
	p.Profile, _ = uuid.Parse(m["profile"])
	p.Item, _ = uuid.Parse(m["item"])
	p.Version, _ = uuid.Parse(m["version"])
	ms, _ := strconv.ParseInt(m["position_ms"], 10, 64)
	p.Position = time.Duration(ms) * time.Millisecond
	started, _ := strconv.ParseInt(m["started"], 10, 64)
	updated, _ := strconv.ParseInt(m["updated"], 10, 64)
	p.Started, p.Updated = time.Unix(started, 0), time.Unix(updated, 0)
	return p, true, nil
}

func (k *KV) EndPlayback(ctx context.Context, id uuid.UUID) error {
	return k.client.Do(ctx, k.client.B().Del().Key(playbackKey(id)).Build()).Error()
}
