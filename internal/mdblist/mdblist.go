// Package mdblist reads ratings from MDBList, which gathers IMDb's, TMDB's, Rotten Tomatoes'
// critics and audience in one answer, with other sites' the server leaves out. Each server uses its
// own free key, set by an admin.
package mdblist

import (
	"cmp"
	"context"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"time"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/kv"
	"github.com/olivertgwalton/photon-server/internal/provider"
)

const baseURL = "https://api.mdblist.com"

// keySetting is the setting that holds the key.
const keySetting = "api_key"

// limit keeps well inside the free tier, which counts requests a day.
var limit = kv.Limit{Every: 200 * time.Millisecond, Burst: 5}

type Client struct {
	base     string
	settings provider.Settings
	api      provider.Client
}

func New(settings provider.Settings, limits kv.Limiter) *Client {
	return &Client{base: baseURL, settings: settings, api: provider.Client{Name: "mdblist", Limits: limits, Limit: limit}}
}

func (c *Client) Info() provider.Info {
	return provider.Info{
		ID: domain.SourceMDBList, Name: "MDBList", Kinds: []domain.ItemKind{domain.ItemMovie, domain.ItemShow},
		Settings: []provider.Setting{{Key: keySetting, Name: "API key", Secret: true, Required: true}},
	}
}

// sites are MDBList's names for the sites it gathers.
var sites = map[string]domain.RatingSite{
	"imdb": domain.SiteIMDb, "tmdb": domain.SiteTMDB, "tomatoes": domain.SiteRottenTomatoes,
	"popcorn": domain.SiteRottenTomatoesAudience,
}

// Ratings answers a title's ratings, found by its IMDb id, else its TMDB id, else its TVDB id.
// MDBList scores every site out of 100 already.
func (c *Client) Ratings(ctx context.Context, kind domain.ItemKind, ids map[domain.Provider]string) ([]domain.Rating, error) {
	set, err := c.settings(ctx)
	if err != nil {
		return nil, err
	}
	key := set[keySetting]
	if key == "" {
		return nil, provider.ErrNotConfigured
	}
	media := map[domain.ItemKind]string{domain.ItemMovie: "movie", domain.ItemShow: "show"}[kind]
	for _, by := range []domain.Provider{domain.ProviderIMDb, domain.ProviderTMDB, domain.ProviderTVDB} {
		id := ids[by]
		if id == "" {
			continue
		}
		ratings, err := c.ratings(ctx, key, string(by)+"/"+media+"/"+url.PathEscape(id))
		if errors.Is(err, provider.ErrNotFound) {
			continue
		}
		return ratings, err
	}
	return nil, nil
}

func (c *Client) ratings(ctx context.Context, key, path string) ([]domain.Rating, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/"+path+"?"+url.Values{"apikey": {key}}.Encode(), nil)
	if err != nil {
		return nil, err
	}
	var body struct {
		Ratings []struct {
			Source string   `json:"source"`
			Score  *float64 `json:"score"`
			Votes  *int     `json:"votes"`
		} `json:"ratings"`
	}
	if err := c.api.Do(req, &body); err != nil {
		return nil, err
	}
	var out []domain.Rating
	for _, r := range body.Ratings {
		site, ok := sites[r.Source]
		if !ok || r.Score == nil || *r.Score <= 0 {
			continue
		}
		rating := domain.Rating{Site: site, Score: min(*r.Score, 100)}
		if r.Votes != nil {
			rating.Votes = *r.Votes
		}
		out = append(out, rating)
	}
	return out, nil
}

// listPage is how many of a list's titles MDBList answers at most at once.
const listPage = 1000

// List answers an MDBList list's films and shows in its order: one MDBList keeps, or mirrors from
// Trakt or IMDb, by its id or as "user/list".
func (c *Client) List(ctx context.Context, id string) ([]domain.Listed, error) {
	set, err := c.settings(ctx)
	if err != nil {
		return nil, err
	}
	key := set[keySetting]
	if key == "" {
		return nil, provider.ErrNotConfigured
	}
	type item struct {
		Rank int `json:"rank"`
		IDs  struct {
			TMDB int    `json:"tmdb"`
			IMDb string `json:"imdb"`
		} `json:"ids"`
	}
	type ranked struct {
		rank int
		domain.Listed
	}
	var all []ranked
	for offset := 0; ; offset += listPage {
		q := url.Values{"apikey": {key}, "limit": {strconv.Itoa(listPage)}, "offset": {strconv.Itoa(offset)}}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/lists/"+id+"/items?"+q.Encode(), nil)
		if err != nil {
			return nil, err
		}
		var body struct {
			Movies []item `json:"movies"`
			Shows  []item `json:"shows"`
		}
		if err := c.api.Do(req, &body); err != nil {
			return nil, err
		}
		for kind, items := range map[domain.ItemKind][]item{domain.ItemMovie: body.Movies, domain.ItemShow: body.Shows} {
			for _, it := range items {
				ids := map[domain.Provider]string{}
				if it.IDs.TMDB != 0 {
					ids[domain.ProviderTMDB] = strconv.Itoa(it.IDs.TMDB)
				}
				if it.IDs.IMDb != "" {
					ids[domain.ProviderIMDb] = it.IDs.IMDb
				}
				all = append(all, ranked{it.Rank, domain.Listed{Kind: kind, IDs: ids}})
			}
		}
		if len(body.Movies)+len(body.Shows) < listPage {
			break
		}
	}
	slices.SortStableFunc(all, func(a, b ranked) int { return cmp.Compare(a.rank, b.rank) })
	out := make([]domain.Listed, len(all))
	for i, r := range all {
		out[i] = r.Listed
	}
	return out, nil
}
