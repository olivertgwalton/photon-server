package kv

import (
	"context"
	"encoding/json"
	"iter"
	"strconv"
	"strings"
	"time"
	"uuid"

	"github.com/valkey-io/valkey-go"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

const playbackPrefix = "photon:play:"

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
			FieldValue("node", p.Node.String()).FieldValue("card", string(card)).Build(),
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
	p.Node, _ = uuid.Parse(m["node"])
	ms, _ := strconv.ParseInt(m["position_ms"], 10, 64)
	p.Position = time.Duration(ms) * time.Millisecond
	started, _ := strconv.ParseInt(m["started"], 10, 64)
	updated, _ := strconv.ParseInt(m["updated"], 10, 64)
	p.Started, p.Updated = time.Unix(started, 0), time.Unix(updated, 0)
	if err := json.Unmarshal([]byte(m["card"]), &p.Card); err != nil {
		return domain.Playback{}, false, err
	}
	return p, true, nil
}

func (k *KV) EndPlayback(ctx context.Context, id uuid.UUID) error {
	return k.client.Do(ctx, k.client.B().Del().Key(playbackKey(id)).Build()).Error()
}

const nodePrefix = "photon:node:"

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
	var out []Node
	for id, err := range k.ids(ctx, nodePrefix) {
		if err != nil {
			return nil, err
		}
		m, err := k.client.Do(ctx, k.client.B().Hgetall().Key(nodeKey(id)).Build()).AsStrMap()
		if err != nil {
			return nil, err
		}
		// One may go quiet between the scan and the read.
		if len(m) == 0 {
			continue
		}
		seen, _ := strconv.ParseInt(m["seen"], 10, 64)
		out = append(out, Node{ID: id, Address: m["address"], Seen: time.Unix(seen, 0)})
	}
	return out, nil
}

// Playbacks answers every playback going on, across the cluster, by scanning their keys: there are
// as many as there are people watching.
func (k *KV) Playbacks(ctx context.Context) ([]domain.Playback, error) {
	var out []domain.Playback
	for id, err := range k.ids(ctx, playbackPrefix) {
		if err != nil {
			return nil, err
		}
		// One may lapse between the scan and the read.
		if p, ok, err := k.Playback(ctx, id); err != nil {
			return nil, err
		} else if ok {
			out = append(out, p)
		}
	}
	return out, nil
}

// ids yields the id each key under prefix ends in.
func (k *KV) ids(ctx context.Context, prefix string) iter.Seq2[uuid.UUID, error] {
	return func(yield func(uuid.UUID, error) bool) {
		var cursor uint64
		for {
			e, err := k.client.Do(ctx, k.client.B().Scan().Cursor(cursor).Match(prefix+"*").Count(100).Build()).AsScanEntry()
			if err != nil {
				yield(uuid.UUID{}, err)
				return
			}
			for _, key := range e.Elements {
				if id, err := uuid.Parse(strings.TrimPrefix(key, prefix)); err == nil && !yield(id, nil) {
					return
				}
			}
			if cursor = e.Cursor; cursor == 0 {
				return
			}
		}
	}
}
