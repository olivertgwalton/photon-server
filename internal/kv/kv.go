package kv

import (
	"cmp"
	"context"
	"strings"
	"uuid"

	"github.com/valkey-io/valkey-go"
)

// KV is one server's keys in a Valkey that other servers may share: each is named under the
// server's id, which every node of a cluster has, as they share its Postgres.
type KV struct {
	client valkey.Client
	ns     string
}

func (k *KV) key(name string) string { return k.ns + name }

// Open takes a valkey:// or valkeys:// URL; credentials and TLS ride in it.
func Open(url string, server uuid.UUID) (*KV, error) {
	opt, err := valkey.ParseURL(url)
	if err != nil {
		return nil, err
	}
	client, err := valkey.NewClient(opt)
	if err != nil {
		return nil, err
	}
	return &KV{client: client, ns: "photon:" + server.String() + ":"}, nil
}

func (k *KV) Close() { k.client.Close() }

// Clear deletes every key of this server's, answering how many, and leaves other servers' alone.
func (k *KV) Clear(ctx context.Context) (int, error) {
	cleared := 0
	var cursor uint64
	for {
		e, err := k.client.Do(ctx, k.client.B().Scan().Cursor(cursor).Match(k.ns+"*").Count(1000).Build()).AsScanEntry()
		if err != nil {
			return cleared, err
		}
		if len(e.Elements) > 0 {
			n, err := k.client.Do(ctx, k.client.B().Unlink().Key(e.Elements...).Build()).AsInt64()
			cleared += int(n)
			if err != nil {
				return cleared, err
			}
		}
		if cursor = e.Cursor; cursor == 0 {
			return cleared, nil
		}
	}
}

func (k *KV) Ping(ctx context.Context) error {
	return k.client.Do(ctx, k.client.B().Ping().Build()).Error()
}

// Version answers the server's version: Valkey's, or Redis's where it is Redis.
func (k *KV) Version(ctx context.Context) (string, error) {
	info, err := k.client.Do(ctx, k.client.B().Info().Section("server").Build()).ToString()
	if err != nil {
		return "", err
	}
	fields := map[string]string{}
	for line := range strings.Lines(info) {
		if name, value, ok := strings.Cut(strings.TrimSpace(line), ":"); ok {
			fields[name] = value
		}
	}
	return cmp.Or(fields["valkey_version"], fields["redis_version"]), nil
}
