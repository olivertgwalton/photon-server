// Package lyrics finds a track's lyrics: an LRC file beside it, else its tags, else the sources its
// library ranks for them, in their order, whose answers are kept so each is asked once.
package lyrics

import (
	"context"
	"errors"
	"log/slog"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/library"
	"github.com/olivertgwalton/photon-server/internal/media"
	"github.com/olivertgwalton/photon-server/internal/provider"
	"github.com/olivertgwalton/photon-server/internal/store"
)

type Parts interface {
	Lyrics(ctx context.Context, part uuid.UUID) (media.Lyrics, error)
}

type Store interface {
	LyricsAsk(ctx context.Context, item uuid.UUID) (store.LyricsAsk, error)
	KeptLyrics(ctx context.Context, item uuid.UUID, source domain.FieldSource) (string, bool, error)
	KeepLyrics(ctx context.Context, item uuid.UUID, source domain.FieldSource, lyrics string) error
}

type Providers interface {
	Get(ctx context.Context, id domain.FieldSource) (provider.Provider, bool, error)
}

type Finder struct {
	Parts     Parts
	Store     Store
	Providers Providers
	Log       *slog.Logger
}

// Find answers the lyrics of the track item, whose version plays from part, or library.ErrNoLyrics.
// A source that cannot be reached is passed over and asked again next time, as nothing is kept of
// it.
func (f Finder) Find(ctx context.Context, item, part uuid.UUID) (media.Lyrics, error) {
	l, err := f.Parts.Lyrics(ctx, part)
	if !errors.Is(err, library.ErrNoLyrics) {
		return l, err
	}
	ask, err := f.Store.LyricsAsk(ctx, item)
	if errors.Is(err, store.ErrNotFound) {
		return media.Lyrics{}, library.ErrNoLyrics
	}
	if err != nil {
		return media.Lyrics{}, err
	}
	for _, source := range ask.Sources {
		text, kept, err := f.Store.KeptLyrics(ctx, item, source)
		if err != nil {
			return media.Lyrics{}, err
		}
		if !kept {
			if text, kept = f.ask(ctx, source, ask.Query); kept {
				if err := f.Store.KeepLyrics(ctx, item, source, text); err != nil {
					return media.Lyrics{}, err
				}
			}
		}
		if l := media.ParseLyrics(text); len(l.Lines) > 0 {
			return l, nil
		}
	}
	return media.Lyrics{}, library.ErrNoLyrics
}

// ask answers what source says of the track, and whether it answered at all.
func (f Finder) ask(ctx context.Context, source domain.FieldSource, q domain.LyricsQuery) (string, bool) {
	p, ok, err := f.Providers.Get(ctx, source)
	if err != nil || !ok {
		return "", false
	}
	l, ok := provider.As[provider.Lyricist](p, domain.CapabilityLyrics)
	if !ok {
		return "", false
	}
	text, err := l.Lyrics(ctx, q)
	if err != nil {
		f.Log.WarnContext(ctx, "lyrics not fetched", slog.String("source", string(source)), slog.Any("err", err))
		return "", false
	}
	return text, true
}
