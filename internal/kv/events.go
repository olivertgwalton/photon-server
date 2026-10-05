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

const (
	scanPrefix = "photon:scan:"
	scanIndex  = "photon:scans"
)

func scanKey(lib uuid.UUID) string { return scanPrefix + lib.String() }

// SaveScan keeps how far a library's scan has got, for ttl unless told again.
func (k *KV) SaveScan(ctx context.Context, p domain.ScanProgress, ttl time.Duration) error {
	key := scanKey(p.Library)
	cmds := k.client.B()
	for _, r := range k.client.DoMulti(ctx,
		cmds.Hset().Key(key).FieldValue().FieldValue("phase", string(p.Phase)).
			FieldValue("done", strconv.Itoa(p.Done)).FieldValue("known", strconv.Itoa(p.Known)).Build(),
		cmds.Expire().Key(key).Seconds(int64(ttl.Seconds())).Build(),
		k.index(scanIndex, p.Library, ttl),
	) {
		if err := r.Error(); err != nil {
			return err
		}
	}
	return nil
}

func (k *KV) EndScan(ctx context.Context, lib uuid.UUID) error {
	_, err := k.end(ctx, scanIndex, scanPrefix, lib)
	return err
}

// Scans answers every scan going on, across the cluster.
func (k *KV) Scans(ctx context.Context) ([]domain.ScanProgress, error) {
	listed, err := k.listed(ctx, scanIndex, scanPrefix)
	if err != nil {
		return nil, err
	}
	out := make([]domain.ScanProgress, len(listed))
	for i, l := range listed {
		out[i] = domain.ScanProgress{Library: l.id, Phase: domain.ScanPhase(l.fields["phase"])}
		out[i].Done, _ = strconv.Atoi(l.fields["done"])
		out[i].Known, _ = strconv.Atoi(l.fields["known"])
	}
	return out, nil
}
