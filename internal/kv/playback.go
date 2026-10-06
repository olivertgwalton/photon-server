package kv

import (
	"context"
	"encoding/json"
	"strconv"
	"time"
	"uuid"

	"github.com/valkey-io/valkey-go"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

const (
	playbackPrefix = "photon:play:"
	playbackIndex  = "photon:plays"
)

func playbackKey(id uuid.UUID) string { return playbackPrefix + id.String() }

// SavePlayback writes a playback session, which lapses after ttl unless written again.
func (k *KV) SavePlayback(ctx context.Context, p domain.Playback, ttl time.Duration) error {
	key := playbackKey(p.ID)
	card, err := json.Marshal(p.Card)
	if err != nil {
		return err
	}
	cmds := k.client.B()
	for _, r := range k.client.DoMulti(ctx,
		cmds.Hset().Key(key).FieldValue().
			FieldValue("profile", p.Profile.String()).FieldValue("item", p.Item.String()).
			FieldValue("version", p.Version.String()).FieldValue("method", string(p.Method)).
			FieldValue("state", string(p.State)).FieldValue("position_ms", strconv.FormatInt(p.Position.Milliseconds(), 10)).
			FieldValue("started", strconv.FormatInt(p.Started.Unix(), 10)).
			FieldValue("updated", strconv.FormatInt(p.Updated.Unix(), 10)).
			FieldValue("reached", string(p.Reached)).FieldValue("node", p.Node.String()).FieldValue("card", string(card)).Build(),
		cmds.Expire().Key(key).Seconds(int64(ttl.Seconds())).Build(),
		k.index(playbackIndex, p.ID, ttl),
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
	p, err := playbackOf(id, m)
	return p, err == nil, err
}

func playbackOf(id uuid.UUID, m map[string]string) (domain.Playback, error) {
	p := domain.Playback{
		ID: id, Method: domain.PlayMethod(m["method"]), State: domain.PlayState(m["state"]), Reached: domain.Reach(m["reached"]),
	}
	p.Profile, _ = uuid.Parse(m["profile"])
	p.Item, _ = uuid.Parse(m["item"])
	p.Version, _ = uuid.Parse(m["version"])
	p.Node, _ = uuid.Parse(m["node"])
	ms, _ := strconv.ParseInt(m["position_ms"], 10, 64)
	p.Position = time.Duration(ms) * time.Millisecond
	started, _ := strconv.ParseInt(m["started"], 10, 64)
	updated, _ := strconv.ParseInt(m["updated"], 10, 64)
	p.Started, p.Updated = time.Unix(started, 0), time.Unix(updated, 0)
	err := json.Unmarshal([]byte(m["card"]), &p.Card)
	return p, err
}

// EndPlayback forgets a playback session, answering false where it had already ended or lapsed:
// of two nodes ending one at once, only one is told it did.
func (k *KV) EndPlayback(ctx context.Context, id uuid.UUID) (bool, error) {
	return k.end(ctx, playbackIndex, playbackPrefix, id)
}

const (
	nodePrefix = "photon:node:"
	nodeIndex  = "photon:nodes"
)

func nodeKey(id uuid.UUID) string { return nodePrefix + id.String() }

// Node is a server node that says where its peers reach it, and when it last said so.
type Node struct {
	ID      uuid.UUID
	Address string
	Seen    time.Time
}

// SetNode says where a server node answers its peers, for ttl unless said again.
func (k *KV) SetNode(ctx context.Context, id uuid.UUID, address string, ttl time.Duration) error {
	key := nodeKey(id)
	cmds := k.client.B()
	for _, r := range k.client.DoMulti(ctx,
		cmds.Hset().Key(key).FieldValue().FieldValue("address", address).
			FieldValue("seen", strconv.FormatInt(time.Now().Unix(), 10)).Build(),
		cmds.Expire().Key(key).Seconds(int64(ttl.Seconds())).Build(),
		k.index(nodeIndex, id, ttl),
	) {
		if err := r.Error(); err != nil {
			return err
		}
	}
	return nil
}

// NodeAddress answers where a node answers its peers, or false for one that has gone quiet.
func (k *KV) NodeAddress(ctx context.Context, id uuid.UUID) (string, bool, error) {
	address, err := k.client.Do(ctx, k.client.B().Hget().Key(nodeKey(id)).Field("address").Build()).ToString()
	if valkey.IsValkeyNil(err) {
		return "", false, nil
	}
	return address, err == nil, err
}

// Nodes answers every node that has said where its peers reach it and not gone quiet.
func (k *KV) Nodes(ctx context.Context) ([]Node, error) {
	listed, err := k.listed(ctx, nodeIndex, nodePrefix)
	if err != nil {
		return nil, err
	}
	out := make([]Node, len(listed))
	for i, l := range listed {
		seen, _ := strconv.ParseInt(l.fields["seen"], 10, 64)
		out[i] = Node{ID: l.id, Address: l.fields["address"], Seen: time.Unix(seen, 0)}
	}
	return out, nil
}

// Playbacks answers every playback going on, across the cluster.
func (k *KV) Playbacks(ctx context.Context) ([]domain.Playback, error) {
	listed, err := k.listed(ctx, playbackIndex, playbackPrefix)
	if err != nil {
		return nil, err
	}
	out := make([]domain.Playback, len(listed))
	for i, l := range listed {
		if out[i], err = playbackOf(l.id, l.fields); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// An index lists the ids of one kind of key, each scored by when it lapses, so they are listed
// without scanning every key in a Valkey that may be shared. A key is the truth: one that lapsed
// or was ended is left out however its index reads.

// index is the command that lists id in index until ttl has passed.
func (k *KV) index(index string, id uuid.UUID, ttl time.Duration) valkey.Completed {
	return k.client.B().Zadd().Key(index).ScoreMember().
		ScoreMember(float64(time.Now().Add(ttl).UnixMilli()), id.String()).Build()
}

// end deletes the key under prefix for id and its place in index, answering whether the key was
// there to delete.
func (k *KV) end(ctx context.Context, index, prefix string, id uuid.UUID) (bool, error) {
	cmds := k.client.B()
	res := k.client.DoMulti(ctx,
		cmds.Del().Key(prefix+id.String()).Build(),
		cmds.Zrem().Key(index).Member(id.String()).Build(),
	)
	if err := res[1].Error(); err != nil {
		return false, err
	}
	n, err := res[0].AsInt64()
	return n == 1, err
}

type listing struct {
	id     uuid.UUID
	fields map[string]string
}

// listed answers the fields of every key index lists that has not lapsed, read together, and
// forgets what has lapsed.
func (k *KV) listed(ctx context.Context, index, prefix string) ([]listing, error) {
	cmds := k.client.B()
	now := strconv.FormatInt(time.Now().UnixMilli(), 10)
	res := k.client.DoMulti(ctx,
		cmds.Zremrangebyscore().Key(index).Min("-inf").Max(now).Build(),
		cmds.Zrange().Key(index).Min("0").Max("-1").Build(),
	)
	if err := res[0].Error(); err != nil {
		return nil, err
	}
	members, err := res[1].AsStrSlice()
	if err != nil || len(members) == 0 {
		return nil, err
	}
	var ids []uuid.UUID
	reads := make(valkey.Commands, 0, len(members))
	for _, m := range members {
		if id, err := uuid.Parse(m); err == nil {
			ids = append(ids, id)
			reads = append(reads, cmds.Hgetall().Key(prefix+m).Build())
		}
	}
	var out []listing
	for i, r := range k.client.DoMulti(ctx, reads...) {
		fields, err := r.AsStrMap()
		if err != nil {
			return nil, err
		}
		if len(fields) > 0 {
			out = append(out, listing{ids[i], fields})
		}
	}
	return out, nil
}
