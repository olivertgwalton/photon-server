package subtitles

import (
	"context"
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
