// Package tvthemes fetches shows' theme tunes from Plex's theme host by their TheTVDB ids, as
// Plex's TV agent and Jellyfin's theme songs plugin do.
package tvthemes

import (
	"context"
	"errors"
	"net/url"
	"os"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/artwork"
	"github.com/olivertgwalton/photon-server/internal/jobs"
	"github.com/olivertgwalton/photon-server/internal/kv"
)

// Host is where Plex keeps its shows' themes, each at {tvdb id}.mp3.
const Host = "https://tvthemes.plexapp.com/"

// limit keeps every node together gentle with a host that publishes no limit.
var limit = kv.Limit{Every: time.Second, Burst: 5}

// missFor is how long a show the host has no theme for goes unasked.
const missFor = 30 * 24 * time.Hour

type store interface {
	ThemeSubject(ctx context.Context, id uuid.UUID) (string, bool, error)
	SaveFetchedTheme(ctx context.Context, show, id uuid.UUID, url string) error
}

type cache interface {
	Sound(ctx context.Context, id uuid.UUID, url string) (*os.File, error)
}

type misses interface {
	kv.Limiter
	NoteThemeMissing(ctx context.Context, tvdb string, ttl time.Duration) error
	ThemeMissing(ctx context.Context, tvdb string) (bool, error)
}

// Fetch keeps a show's theme from the host at base in the cache, for good, where the show still
// wants one. A show the host has none for is not asked about again for a month.
func Fetch(st store, c cache, m misses, base string) jobs.Handler {
	return func(ctx context.Context, show uuid.UUID) error {
		tvdb, ok, err := st.ThemeSubject(ctx, show)
		if err != nil || !ok {
			return err
		}
		if missing, err := m.ThemeMissing(ctx, tvdb); err != nil || missing {
			return err
		}
		if err := kv.Wait(ctx, m, "tvthemes", limit); err != nil {
			return err
		}
		id, address := uuid.NewV7(), base+url.PathEscape(tvdb)+".mp3"
		f, err := c.Sound(ctx, id, address)
		if errors.Is(err, artwork.ErrMissing) {
			return m.NoteThemeMissing(ctx, tvdb, missFor)
		}
		if err != nil {
			return err
		}
		f.Close()
		return st.SaveFetchedTheme(ctx, show, id, address)
	}
}
