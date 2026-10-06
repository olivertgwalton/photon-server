package kv

import (
	"context"
	"time"
)

// themeMisses are the titles ThemerrDB had no theme for and the YouTube links that refused, each
// forgotten after a while so that a theme listed since, or a link that answers again, is fetched.
const themeMisses = "theme_misses"

func (k *KV) NoteThemeMissing(ctx context.Context, key string, ttl time.Duration) error {
	return k.client.Do(ctx, k.client.B().Hsetex().Key(k.key(themeMisses)).Px(ttl.Milliseconds()).Fields().Numfields(1).
		FieldValue().FieldValue(key, "1").Build()).Error()
}

func (k *KV) ThemeMissing(ctx context.Context, key string) (bool, error) {
	return k.client.Do(ctx, k.client.B().Hexists().Key(k.key(themeMisses)).Field(key).Build()).AsBool()
}
