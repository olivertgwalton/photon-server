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
func (c *Client) Match(ctx context.Context, loc domain.Locale, _ domain.ItemKind, h provider.Hints) (string, error) {
	id, err := provider.Resolve(h, domain.ProviderTVDB, []domain.Provider{domain.ProviderIMDb, domain.ProviderTMDB},
		func(_ domain.Provider, v string) ([]domain.Candidate, error) { return c.Find(ctx, v) },
		func(title string, year int) ([]domain.Candidate, error) { return c.Search(ctx, loc, title, year) })
	return id, err
}

// Describe answers TheTVDB's details of a show and of the seasons asked for, in the order its
// files are numbered in, with the episodes yet to air.
func (c *Client) Describe(ctx context.Context, loc domain.Locale, _ domain.ItemKind, id string, seasons domain.SeasonRequest) (domain.Metadata, map[int]domain.SeasonMetadata, error) {
	n, err := strconv.Atoi(id)
	if err != nil {
		return domain.Metadata{}, nil, err
	}
	m, err := c.Details(ctx, loc, n)
	if err != nil {
		return domain.Metadata{}, nil, err
	}
	said, err := c.Seasons(ctx, loc, n, seasons.Numbers, seasons.Order)
	return m, said, err
}

// Candidates answers TheTVDB's shows by a name, for an admin fixing a match.
func (c *Client) Candidates(ctx context.Context, loc domain.Locale, _ domain.ItemKind, title string, year int) ([]domain.Candidate, error) {
	return c.Search(ctx, loc, title, year)
}
