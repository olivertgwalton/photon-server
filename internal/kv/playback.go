package kv

import (
	"context"
	"encoding/json"
	"time"
	"uuid"

	"github.com/valkey-io/valkey-go"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// Playbacks, nodes and scans are each one hash, a record to a field under its id, and each field
// lapses on its own (Valkey 9's hash field expiry). A record and its place in the list are one, so
// nothing lists a record that has lapsed or ended, and a Valkey that other things share is never
// scanned.
const (
	playbacks = "plays"
	nodes     = "nodes"
	scans     = "scans"
)

// keep writes v as id's record in set, lapsing after ttl unless written again.
func (k *KV) keep(ctx context.Context, set string, id uuid.UUID, v any, ttl time.Duration) error {
	record, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return k.client.Do(ctx, k.client.B().Hsetex().Key(k.key(set)).Px(ttl.Milliseconds()).Fields().Numfields(1).
		FieldValue().FieldValue(id.String(), string(record)).Build()).Error()
}

// kept reads id's record in set into v, answering false for none.
func (k *KV) kept(ctx context.Context, set string, id uuid.UUID, v any) (bool, error) {
	record, err := k.client.Do(ctx, k.client.B().Hget().Key(k.key(set)).Field(id.String()).Build()).AsBytes()
	if valkey.IsValkeyNil(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, json.Unmarshal(record, v)
}

// forget deletes id's record in set, answering whether it was there: of two nodes forgetting one
// at once, only one is told it did.
func (k *KV) forget(ctx context.Context, set string, id uuid.UUID) (bool, error) {
	n, err := k.client.Do(ctx, k.client.B().Hdel().Key(k.key(set)).Field(id.String()).Build()).AsInt64()
	return n == 1, err
}

// every answers each record in set.
func every[T any](ctx context.Context, k *KV, set string) ([]T, error) {
	records, err := k.client.Do(ctx, k.client.B().Hvals().Key(k.key(set)).Build()).AsStrSlice()
	if err != nil {
		return nil, err
	}
	out := make([]T, len(records))
	for i, r := range records {
		if err := json.Unmarshal([]byte(r), &out[i]); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// ClaimPlayback writes a new playback session, which lapses after ttl unless written again, if no
// playback is kept under its id: of nodes claiming one id at once, only one is told it did.
func (k *KV) ClaimPlayback(ctx context.Context, p domain.Playback, ttl time.Duration) (bool, error) {
	record, err := json.Marshal(p)
	if err != nil {
		return false, err
	}
	n, err := k.client.Do(ctx, k.client.B().Hsetex().Key(k.key(playbacks)).Fnx().Px(ttl.Milliseconds()).Fields().
		Numfields(1).FieldValue().FieldValue(p.ID.String(), string(record)).Build()).AsInt64()
	return n == 1, err
}

// SavePlayback writes a playback session, which lapses after ttl unless written again.
func (k *KV) SavePlayback(ctx context.Context, p domain.Playback, ttl time.Duration) error {
	return k.keep(ctx, playbacks, p.ID, p, ttl)
}

// Playback answers a playback session, or false for one that has ended or lapsed.
func (k *KV) Playback(ctx context.Context, id uuid.UUID) (domain.Playback, bool, error) {
	var p domain.Playback
	ok, err := k.kept(ctx, playbacks, id, &p)
	return p, ok, err
}

// EndPlayback forgets a playback session, answering false where it had already ended or lapsed.
func (k *KV) EndPlayback(ctx context.Context, id uuid.UUID) (bool, error) {
	return k.forget(ctx, playbacks, id)
}

// Playbacks answers every playback going on, across the cluster.
func (k *KV) Playbacks(ctx context.Context) ([]domain.Playback, error) {
	return every[domain.Playback](ctx, k, playbacks)
}

// Node is a server node that says where its peers reach it, and when it last said so.
type Node struct {
	ID      uuid.UUID
	Address string
	Seen    time.Time
}

// SetNode says where a server node answers its peers, for ttl unless said again.
func (k *KV) SetNode(ctx context.Context, id uuid.UUID, address string, ttl time.Duration) error {
	return k.keep(ctx, nodes, id, Node{ID: id, Address: address, Seen: time.Now()}, ttl)
}

// NodeAddress answers where a node answers its peers, or false for one that has gone quiet.
func (k *KV) NodeAddress(ctx context.Context, id uuid.UUID) (string, bool, error) {
	var n Node
	ok, err := k.kept(ctx, nodes, id, &n)
	return n.Address, ok, err
}

// Nodes answers every node that has said where its peers reach it and not gone quiet.
func (k *KV) Nodes(ctx context.Context) ([]Node, error) {
	return every[Node](ctx, k, nodes)
}
