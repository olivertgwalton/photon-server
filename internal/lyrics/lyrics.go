// Package lyrics finds a track's lyrics: an LRC file beside it, else its tags, else what the
// sources its library ranks for them found, in their order. The sources are asked by a job, ahead
// of anyone wanting them, as a library's other sources are, and their answers kept.
package lyrics

import (
	"context"
	"errors"
	"fmt"
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
	KeepLyrics(ctx context.Context, item uuid.UUID, source domain.FieldSource, lyrics string, timing domain.LyricsTiming) error
	AskLyrics(ctx context.Context, item uuid.UUID) error
}

type Providers interface {
	Get(ctx context.Context, id domain.FieldSource) (provider.Provider, bool, error)
}

type Finder struct {
	Parts     Parts
	Store     Store
	Providers Providers
}

// Find answers the lyrics of the track item, whose version plays from part, or library.ErrNoLyrics:
// its own, else the first its library's sources found. It asks no source itself; where one has not
// answered, or its answer is no longer current, Fetch is queued to ask it.
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
	found := media.Lyrics{}
	for _, source := range ask.Sources {
		text, current, err := f.Store.KeptLyrics(ctx, item, source)
		if err != nil {
			return media.Lyrics{}, err
		}
		if !current {
			if err := f.Store.AskLyrics(ctx, item); err != nil {
				return media.Lyrics{}, err
			}
		}
		if l := media.ParseLyrics(text); len(l.Lines) > 0 {
			found = l
			break
		}
	}
	if len(found.Lines) == 0 {
		return media.Lyrics{}, library.ErrNoLyrics
	}
	return found, nil
}

// Fetch asks the sources a track's library ranks for its lyrics, in their order, until one finds
// some, keeping each answer: one whose answer is current is not asked again. A source that cannot
// be reached keeps nothing, and fails the job where no source found any, so it is asked again.
func (f Finder) Fetch(ctx context.Context, item uuid.UUID) error {
	ask, err := f.Store.LyricsAsk(ctx, item)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	var failed error
	for _, source := range ask.Sources {
		text, current, err := f.Store.KeptLyrics(ctx, item, source)
		if err != nil {
			return err
		}
		if !current {
			answer, err := f.ask(ctx, source, ask.Query)
			if err != nil {
				failed = fmt.Errorf("%s: %w", source, err)
				continue
			}
			if err := f.Store.KeepLyrics(ctx, item, source, answer, media.TimingOf(answer)); err != nil {
				return err
			}
			text = answer
		}
		if media.TimingOf(text) != "" {
			return nil
		}
	}
	return failed
}

// ask answers what source says of the track: "" for none, and none for a source that is gone or
// finds no lyrics.
func (f Finder) ask(ctx context.Context, source domain.FieldSource, q domain.LyricsQuery) (string, error) {
	p, ok, err := f.Providers.Get(ctx, source)
	if err != nil || !ok {
		return "", err
	}
	l, ok := provider.As[provider.Lyricist](p, domain.CapabilityLyrics)
	if !ok {
		return "", nil
	}
	return l.Lyrics(ctx, q)
}
