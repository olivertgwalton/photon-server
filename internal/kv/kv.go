package kv

import (
	"cmp"
	"context"
	"strings"

	"github.com/valkey-io/valkey-go"
)

type KV struct {
	client valkey.Client
}

// Open takes a valkey:// or valkeys:// URL; credentials and TLS ride in it.
func Open(url string) (*KV, error) {
	opt, err := valkey.ParseURL(url)
	if err != nil {
		return nil, err
	}
	client, err := valkey.NewClient(opt)
	if err != nil {
		return nil, err
	}
	return &KV{client: client}, nil
}

func (k *KV) Close() { k.client.Close() }

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
