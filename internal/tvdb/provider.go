package tvdb

import (
	"context"
	"strconv"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/provider"
)

func (c *Client) Info() provider.Info {
	return provider.Info{ID: domain.SourceTVDB, Name: "TheTVDB", Kinds: []domain.ItemKind{domain.ItemShow}}
}

// Match finds a show on TheTVDB by its TVDB id, else the show an IMDb or TMDB id names, else a
// confident search.
func (c *Client) Match(ctx context.Context, _ domain.ItemKind, h provider.Hints) (string, error) {
	id, err := provider.Resolve(h, domain.ProviderTVDB, []domain.Provider{domain.ProviderIMDb, domain.ProviderTMDB},
		func(_ domain.Provider, v string) ([]domain.Candidate, error) { return c.Find(ctx, v) },
		func(title string, year int) ([]domain.Candidate, error) { return c.Search(ctx, title, year) })
	if err != nil || id == 0 {
		return "", err
	}
	return strconv.Itoa(id), nil
}

// Describe answers TheTVDB's details of a show and of the seasons asked for.
func (c *Client) Describe(ctx context.Context, _ domain.ItemKind, id string, seasons []int) (domain.Metadata, map[int]domain.SeasonMetadata, error) {
	n, err := strconv.Atoi(id)
	if err != nil {
		return domain.Metadata{}, nil, err
	}
	m, err := c.Details(ctx, n)
	if err != nil {
		return domain.Metadata{}, nil, err
	}
	said, err := c.Seasons(ctx, n, seasons)
	return m, said, err
}
