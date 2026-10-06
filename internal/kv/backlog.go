package kv

import (
	"context"
	"time"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// backlogs is a hash of how many jobs of each kind are done since its backlog began, a field to a
// kind, each lapsing on its own.
const backlogs = "backlogs"

// JobDone counts one more job of kind done, answering how many are, and keeps the count for ttl
// from now.
func (k *KV) JobDone(ctx context.Context, kind domain.JobKind, ttl time.Duration) (int, error) {
	res := k.client.DoMulti(ctx,
		k.client.B().Hincrby().Key(k.key(backlogs)).Field(string(kind)).Increment(1).Build(),
		k.client.B().Hexpire().Key(k.key(backlogs)).Seconds(int64(ttl.Seconds())).Fields().Numfields(1).Field(string(kind)).Build(),
	)
	if err := res[1].Error(); err != nil {
		return 0, err
	}
	done, err := res[0].AsInt64()
	return int(done), err
}

// JobsDone answers how many jobs of each kind with a backlog are done since it began.
func (k *KV) JobsDone(ctx context.Context) (map[domain.JobKind]int, error) {
	counts, err := k.client.Do(ctx, k.client.B().Hgetall().Key(k.key(backlogs)).Build()).AsIntMap()
	out := make(map[domain.JobKind]int, len(counts))
	for kind, n := range counts {
		out[domain.JobKind(kind)] = int(n)
	}
	return out, err
}

// EndBacklog starts kind's count of jobs done again.
func (k *KV) EndBacklog(ctx context.Context, kind domain.JobKind) error {
	return k.client.Do(ctx, k.client.B().Hdel().Key(k.key(backlogs)).Field(string(kind)).Build()).Error()
}
