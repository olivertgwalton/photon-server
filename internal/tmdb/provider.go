package tmdb

import (
	"context"
	"errors"
	"strconv"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/provider"
)

var kinds = map[domain.ItemKind]Kind{domain.ItemMovie: Movie, domain.ItemShow: Show}

func (c *Client) Info() provider.Info {
	return provider.Info{ID: domain.SourceTMDB, Name: "TMDB", Kinds: []domain.ItemKind{domain.ItemMovie, domain.ItemShow}}
}

// Match finds a title on TMDB by its TMDB id, else the title an IMDb or TVDB id names, else a
// confident search.
func (c *Client) Match(ctx context.Context, loc domain.Locale, kind domain.ItemKind, h provider.Hints) (string, error) {
	k := kinds[kind]
	id, err := provider.Resolve(h, domain.ProviderTMDB, []domain.Provider{domain.ProviderIMDb, domain.ProviderTVDB},
		func(p domain.Provider, v string) ([]domain.Candidate, error) { return c.Find(ctx, loc, k, p, v) },
		func(title string, year int) ([]domain.Candidate, error) { return c.Search(ctx, loc, k, title, year) })
	return id, err
}

// Describe answers TMDB's details of a title, its score among them, and of the seasons of a show
// asked for that TMDB has. TMDB numbers episodes as aired, so a show numbered otherwise is
// described without its seasons.
func (c *Client) Describe(ctx context.Context, loc domain.Locale, kind domain.ItemKind, id string, seasons domain.SeasonRequest) (domain.Metadata, map[int]domain.SeasonMetadata, error) {
	n, err := strconv.Atoi(id)
	if err != nil {
		return domain.Metadata{}, nil, err
	}
	m, err := c.Details(ctx, loc, kinds[kind], n)
	if err != nil {
		return domain.Metadata{}, nil, err
	}
	said := map[int]domain.SeasonMetadata{}
	if seasons.Order != domain.OrderAired {
		return m, said, nil
	}
	for _, number := range seasons.Numbers {
		s, err := c.Season(ctx, loc, n, number)
		if errors.Is(err, provider.ErrNotFound) {
			continue
		}
		if err != nil {
			return domain.Metadata{}, nil, err
		}
		said[number] = s
	}
	return m, said, nil
}

// DescribePerson answers what TMDB knows of someone with a TMDB id; provider.ErrNotFound for anyone
// else.
func (c *Client) DescribePerson(ctx context.Context, loc domain.Locale, ids map[domain.Provider]string) (domain.Person, error) {
	id := ids[domain.ProviderTMDB]
	if id == "" {
		return domain.Person{}, provider.ErrNotFound
	}
	return c.Person(ctx, loc, id)
}

// Candidates answers TMDB's titles by a name, for an admin fixing a match.
func (c *Client) Candidates(ctx context.Context, loc domain.Locale, kind domain.ItemKind, title string, year int) ([]domain.Candidate, error) {
	return c.Search(ctx, loc, kinds[kind], title, year)
}
