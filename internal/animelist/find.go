package animelist

import (
	"context"
	"strconv"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// Lists are the anime lists kept.
type Lists interface {
	AnimeShow(ctx context.Context, p domain.Provider, show int) ([]Entry, error)
	AnimeEntry(ctx context.Context, p domain.Provider, id string) (Entry, bool, error)
}

// Find answers the entry a title opens with, by the ids it carries: its own AniList or AniDB id,
// a show's TheTVDB or TMDB id, or a film's TMDB or IMDb id; false for a title the lists do not
// name, which is not anime.
func Find(ctx context.Context, l Lists, kind domain.ItemKind, ids map[domain.Provider]string) (Entry, bool, error) {
	tries := []domain.Provider{domain.ProviderAniList, domain.ProviderAniDB}
	if kind == domain.ItemShow {
		for _, p := range []domain.Provider{domain.ProviderTVDB, domain.ProviderTMDB} {
			show, err := strconv.Atoi(ids[p])
			if err != nil {
				continue
			}
			entries, err := l.AnimeShow(ctx, p, show)
			if err != nil {
				return Entry{}, false, err
			}
			if e, ok := Opening(entries, p); ok {
				return e, true, nil
			}
		}
	} else {
		tries = append(tries, domain.ProviderTMDB, domain.ProviderIMDb)
	}
	for _, p := range tries {
		if id := ids[p]; id != "" {
			if e, ok, err := l.AnimeEntry(ctx, p, id); err != nil || ok {
				return e, ok, err
			}
		}
	}
	return Entry{}, false, nil
}
