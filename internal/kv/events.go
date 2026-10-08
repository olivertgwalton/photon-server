package kv

import (
	"context"
	"time"
	"uuid"

	"github.com/valkey-io/valkey-go"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// events carries every node's events to every node's streams.
const events = "events"

func (k *KV) PublishEvent(ctx context.Context, message string) error {
	return k.client.Do(ctx, k.client.B().Publish().Channel(k.key(events)).Message(message).Build()).Error()
}

// ReceiveEvents hands fn each event published, until ctx ends or the connection is lost.
func (k *KV) ReceiveEvents(ctx context.Context, fn func(message string)) error {
	return k.client.Receive(ctx, k.client.B().Subscribe().Channel(k.key(events)).Build(), func(m valkey.PubSubMessage) {
		fn(m.Message)
	})
}

// SaveScan keeps how far a library's scan has got, for ttl unless told again.
func (k *KV) SaveScan(ctx context.Context, p domain.ScanProgress, ttl time.Duration) error {
	return keep(ctx, k, scans, p.Library, p, ttl)
}

func (k *KV) EndScan(ctx context.Context, lib uuid.UUID) error {
	_, err := k.forget(ctx, scans, lib)
	return err
}

// Scans answers every scan going on, across the cluster.
func (k *KV) Scans(ctx context.Context) ([]domain.ScanProgress, error) {
	return every[domain.ScanProgress](ctx, k, scans)
}
