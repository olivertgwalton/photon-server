package tmdb

import (
	"context"
	"errors"
	"net/url"
	"slices"
	"strconv"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/provider"
)

var kinds = map[domain.ItemKind]Kind{domain.ItemMovie: Movie, domain.ItemShow: Show}

func (c *Client) Info() provider.Info {
	return provider.Info{
		ID: domain.SourceTMDB, Name: "TMDB", Kinds: []domain.ItemKind{domain.ItemMovie, domain.ItemShow},
		Settings: []provider.Setting{{Key: tokenSetting, Name: "API read access token", Secret: true}},
	}
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
// asked for that TMDB has (every one, where every one is asked for), with the season its next
// episode to air is in, so the episodes yet to air are known. TMDB numbers episodes as aired, so a show numbered otherwise is described without
// its seasons.
func (c *Client) Describe(ctx context.Context, loc domain.Locale, kind domain.ItemKind, id string, seasons domain.SeasonRequest) (domain.Metadata, map[int]domain.SeasonMetadata, error) {
	n, err := strconv.Atoi(id)
	if err != nil {
		return domain.Metadata{}, nil, err
	}
	m, shown, err := c.Details(ctx, loc, kinds[kind], n)
	if err != nil {
		return domain.Metadata{}, nil, err
	}
	said := map[int]domain.SeasonMetadata{}
	if seasons.Order != domain.OrderAired {
		return m, said, nil
	}
	numbers := slices.Concat(seasons.Numbers, shown.Airing)
	if seasons.Scope == domain.SeasonsEvery {
		numbers = shown.Every
	}
	slices.Sort(numbers)
	for _, number := range slices.Compact(numbers) {
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

// List answers a TMDB list's films and shows in its order, a page at a time.
func (c *Client) List(ctx context.Context, id string) ([]domain.Listed, error) {
	var out []domain.Listed
	for page := 1; ; page++ {
		var body struct {
			Items []struct {
				result
				MediaType string `json:"media_type"`
			} `json:"items"`
			TotalPages int `json:"total_pages"`
		}
		q := url.Values{"page": {strconv.Itoa(page)}}
		if err := c.get(ctx, domain.Locale{}, "/list/"+url.PathEscape(id), q, &body); err != nil {
			return nil, err
		}
		for _, item := range body.Items {
			kind, ok := map[string]domain.ItemKind{"movie": domain.ItemMovie, "tv": domain.ItemShow}[item.MediaType]
			if ok {
				m := item.match()
				out = append(out, domain.Listed{Kind: kind, IDs: map[domain.Provider]string{domain.ProviderTMDB: m.ID}, Title: m.Title, Year: m.Year})
			}
		}
		if page >= body.TotalPages {
			return out, nil
		}
	}
}
