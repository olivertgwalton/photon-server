package kv

import (
	"context"
	"strconv"
	"time"
	"uuid"

	"github.com/valkey-io/valkey-go"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// eventsChannel carries every node's events to every node's streams.
const eventsChannel = "photon:events"

func (k *KV) PublishEvent(ctx context.Context, message string) error {
	return k.client.Do(ctx, k.client.B().Publish().Channel(eventsChannel).Message(message).Build()).Error()
}

// ReceiveEvents hands fn each event published, until ctx ends or the connection is lost.
func (k *KV) ReceiveEvents(ctx context.Context, fn func(message string)) error {
	return k.client.Receive(ctx, k.client.B().Subscribe().Channel(eventsChannel).Build(), func(m valkey.PubSubMessage) {
		fn(m.Message)
	})
}

const scanPrefix = "photon:scan:"

func scanKey(lib uuid.UUID) string { return scanPrefix + lib.String() }

// SaveScan keeps how far a library's scan has got, for ttl unless told again.
func (k *KV) SaveScan(ctx context.Context, p domain.ScanProgress, ttl time.Duration) error {
	key := scanKey(p.Library)
	cmds := k.client.B()
	for _, r := range k.client.DoMulti(ctx,
		cmds.Hset().Key(key).FieldValue().FieldValue("phase", string(p.Phase)).
			FieldValue("done", strconv.Itoa(p.Done)).FieldValue("known", strconv.Itoa(p.Known)).Build(),
		cmds.Expire().Key(key).Seconds(int64(ttl.Seconds())).Build(),
	) {
		if err := r.Error(); err != nil {
			return err
		}
	}
	return nil
}

func (k *KV) EndScan(ctx context.Context, lib uuid.UUID) error {
	return k.client.Do(ctx, k.client.B().Del().Key(scanKey(lib)).Build()).Error()
}

// Scans answers every scan going on, across the cluster.
func (k *KV) Scans(ctx context.Context) ([]domain.ScanProgress, error) {
	var out []domain.ScanProgress
	for lib, err := range k.ids(ctx, scanPrefix) {
		if err != nil {
			return nil, err
		}
		m, err := k.client.Do(ctx, k.client.B().Hgetall().Key(scanKey(lib)).Build()).AsStrMap()
		if err != nil {
			return nil, err
		}
		if len(m) == 0 {
			continue
		}
		p := domain.ScanProgress{Library: lib, Phase: domain.ScanPhase(m["phase"])}
		p.Done, _ = strconv.Atoi(m["done"])
		p.Known, _ = strconv.Atoi(m["known"])
		out = append(out, p)
	}
	return out, nil
}
