package tmdb

import (
	"cmp"
	"context"
	"errors"
	"net/url"
	"slices"
	"strconv"
	"strings"

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

// charts are TMDB's charts, read as lists by their paths: what each is called, and what it holds.
// A trending chart of both says of each title which it is.
type chart struct {
	path, title string
	holds       domain.ItemKind
}

var charts = []chart{
	{"trending/all/day", "Trending today", ""},
	{"trending/all/week", "Trending this week", ""},
	{"trending/movie/day", "Films trending today", domain.ItemMovie},
	{"trending/movie/week", "Films trending this week", domain.ItemMovie},
	{"trending/tv/day", "Shows trending today", domain.ItemShow},
	{"trending/tv/week", "Shows trending this week", domain.ItemShow},
	{"movie/popular", "Popular films", domain.ItemMovie},
	{"tv/popular", "Popular shows", domain.ItemShow},
	{"movie/top_rated", "Top rated films", domain.ItemMovie},
	{"tv/top_rated", "Top rated shows", domain.ItemShow},
	{"movie/now_playing", "Films in cinemas", domain.ItemMovie},
	{"movie/upcoming", "Films coming to cinemas", domain.ItemMovie},
	{"tv/airing_today", "Shows on today", domain.ItemShow},
	{"tv/on_the_air", "Shows on this week", domain.ItemShow},
}

// Lists answers TMDB's charts, those named like query. TMDB has no search of its users' lists.
func (c *Client) Lists(_ context.Context, query string) ([]domain.KeptList, error) {
	var out []domain.KeptList
	for _, ch := range charts {
		if strings.Contains(strings.ToLower(ch.title), strings.ToLower(query)) {
			out = append(out, domain.KeptList{ID: ch.path, Title: ch.title, About: "TMDB's chart, its first 100 titles", Holds: ch.holds})
		}
	}
	return out, nil
}

// chartPages is how much of a chart is read: its first 100 titles. TMDB pages its popular
// charts through every title it has.
const chartPages = 5

// List answers a TMDB list's films and shows in its order, a page at a time; or one of its charts,
// named by its path.
func (c *Client) List(ctx context.Context, id string) ([]domain.Listed, error) {
	i := slices.IndexFunc(charts, func(ch chart) bool { return ch.path == id })
	isChart := i >= 0
	var chart domain.ItemKind
	path := "/list/" + url.PathEscape(id)
	if isChart {
		chart, path = charts[i].holds, "/"+id
	}
	kinds := map[string]domain.ItemKind{"movie": domain.ItemMovie, "tv": domain.ItemShow}
	var out []domain.Listed
	for page := 1; ; page++ {
		type item struct {
			result
			MediaType string `json:"media_type"`
		}
		var body struct {
			Items      []item `json:"items"`
			Results    []item `json:"results"`
			TotalPages int    `json:"total_pages"`
		}
		q := url.Values{"page": {strconv.Itoa(page)}}
		if err := c.get(ctx, domain.Locale{}, path, q, &body); err != nil {
			return nil, err
		}
		for _, it := range append(body.Items, body.Results...) {
			kind := cmp.Or(chart, kinds[it.MediaType])
			if kind == "" {
				continue
			}
			m := it.match()
			out = append(out, domain.Listed{Kind: kind, IDs: map[domain.Provider]string{domain.ProviderTMDB: m.ID}, Title: m.Title, Year: m.Year})
		}
		if page >= body.TotalPages || isChart && page >= chartPages {
			return out, nil
		}
	}
}
