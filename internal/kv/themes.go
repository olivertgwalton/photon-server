package kv

import (
	"context"
	"time"
)

// themeMisses are the shows Plex's theme host had no theme for, by TheTVDB id, each forgotten
// after a while so that a theme added there since is found.
const themeMisses = "theme_misses"

func (k *KV) NoteThemeMissing(ctx context.Context, tvdb string, ttl time.Duration) error {
	return k.client.Do(ctx, k.client.B().Hsetex().Key(k.key(themeMisses)).Px(ttl.Milliseconds()).Fields().Numfields(1).
		FieldValue().FieldValue(tvdb, "1").Build()).Error()
}

func (k *KV) ThemeMissing(ctx context.Context, tvdb string) (bool, error) {
	return k.client.Do(ctx, k.client.B().Hexists().Key(k.key(themeMisses)).Field(tvdb).Build()).AsBool()
}
