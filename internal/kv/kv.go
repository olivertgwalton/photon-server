package kv

import (
	"context"

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
