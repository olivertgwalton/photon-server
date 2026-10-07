package subtitles

import (
	"context"
	"errors"
	"slices"
	"time"
	"uuid"

	"golang.org/x/text/language"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/library"
	"github.com/olivertgwalton/photon-server/internal/provider"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// Fetcher is the store, with subtitles for its copies found and fetched from the providers.
type Fetcher struct {
	*store.Store
	providers *provider.Registry
}

func NewFetcher(st *store.Store, providers *provider.Registry) Fetcher {
	return Fetcher{Store: st, providers: providers}
}

// Search answers the copy of a film or an episode searched, the one asked for else its longest,
// and the subtitles the providers have for it in a language: those made for its very file first.
func (f Fetcher) Search(ctx context.Context, profile, item, version uuid.UUID, lang language.Tag) (uuid.UUID, []domain.FoundSubtitle, error) {
	s, err := f.SubtitleSearchOf(ctx, profile, item, version)
	if err != nil {
		return uuid.UUID{}, nil, err
	}
	found, err := f.providers.SearchSubtitles(ctx, query(s, lang))
	return s.Version, found, err
}

// query is what a copy's subtitles are searched by in a language: its release is hashed where it
// is one file, as one file of several is no release on its own.
func query(s store.SubtitleSearch, lang language.Tag) domain.SubtitleQuery {
	q := s.Query
	q.Language = lang
	if s.Parts == 1 {
		if file, err := library.Open(s.Root, s.RelPath); err == nil {
			q.Hash, _ = library.MovieHash(file)
			_ = file.Close()
		}
	}
	return q
}

// Fetch fetches a subtitle a search found for a copy of a film or an episode and keeps it beside
// the copy, for every profile; it answers the subtitle's id.
func (f Fetcher) Fetch(ctx context.Context, profile, item, version uuid.UUID, s domain.FoundSubtitle) (uuid.UUID, error) {
	search, err := f.SubtitleSearchOf(ctx, profile, item, version)
	if err != nil {
		return uuid.UUID{}, err
	}
	body, err := f.providers.FetchSubtitle(ctx, s.Source, s.ID)
	if err != nil {
		return uuid.UUID{}, err
	}
	return f.SaveFetchedSubtitle(ctx, search.Version, store.FetchedSubtitleFile{
		Language: s.Language, Title: s.Release, HearingImpaired: s.HearingImpaired, Forced: s.Forced, Body: body,
	})
}

// searchAgainAfter is how long a copy with no subtitle to be had in a language waits before it is
// searched again: most are written in a release's first weeks, and searching daily would spend
// the provider's requests on titles that have none.
const searchAgainAfter = 7 * 24 * time.Hour

// wantedBatch is how many copies' wants are read at once.
const wantedBatch = 100

// FetchMissing fetches, for each copy of a film or an episode with no subtitle in a language its
// library names, the one its library takes, as Jellyfin's "Download missing subtitles" does:
// newest copies first, and only one made for the very file where the library asks for a match. It
// stops for the day at the provider's quota, and answers how many it fetched.
func (f Fetcher) FetchMissing(ctx context.Context) (int, error) {
	fetched := 0
	var after *store.WantedSubtitle
	for {
		wanted, err := f.WantedSubtitles(ctx, time.Now().Add(-searchAgainAfter), after, wantedBatch)
		if err != nil || len(wanted) == 0 {
			return fetched, err
		}
		for _, w := range wanted {
			got, err := f.fetchWanted(ctx, w)
			if errors.Is(err, provider.ErrQuota) {
				return fetched, nil
			}
			if err != nil {
				return fetched, err
			}
			if got {
				fetched++
			}
		}
		after = &wanted[len(wanted)-1]
	}
}

func (f Fetcher) fetchWanted(ctx context.Context, w store.WantedSubtitle) (bool, error) {
	_, found, err := f.Search(ctx, uuid.UUID{}, w.Item, w.Version, w.Language)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return false, err
	}
	// A forced subtitle covers only what is not in the copy's own language.
	i := slices.IndexFunc(found, func(s domain.FoundSubtitle) bool {
		return !s.Forced && (s.ForRelease || w.Match == domain.SubtitleMatchAny)
	})
	if i >= 0 {
		if _, err := f.Fetch(ctx, uuid.UUID{}, w.Item, w.Version, found[i]); err != nil {
			return false, err
		}
	}
	return i >= 0, f.SubtitleSearched(ctx, w.Version, w.Language)
}
