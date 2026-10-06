// Package mdblist reads ratings from MDBList, which gathers IMDb's, TMDB's, Rotten Tomatoes'
// critics and audience, Metacritic's, Letterboxd's and Trakt's in one answer. Each server uses its
// own free key, set by an admin.
package mdblist

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
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

var errNotFound = errors.New("mdblist: not found")

type limiter interface {
	Allow(ctx context.Context, key string, l kv.Limit) (time.Duration, error)
}

type Client struct {
	base     string
	settings provider.Settings
	http     *http.Client
	limits   limiter
}

func New(settings provider.Settings, limits limiter) *Client {
	return &Client{base: baseURL, settings: settings, http: &http.Client{Timeout: 30 * time.Second}, limits: limits}
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
	"popcorn": domain.SiteRottenTomatoesAudience, "metacritic": domain.SiteMetacritic,
	"letterboxd": domain.SiteLetterboxd, "trakt": domain.SiteTrakt,
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
		if errors.Is(err, errNotFound) {
			continue
		}
		return ratings, err
	}
	return nil, nil
}

func (c *Client) ratings(ctx context.Context, key, path string) ([]domain.Rating, error) {
	if err := kv.Wait(ctx, c.limits, "mdblist", limit); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/"+path+"?"+url.Values{"apikey": {key}}.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := provider.Send(c.http, req)
	if ue, ok := errors.AsType[*url.Error](err); ok {
		// Its address carries the key.
		return nil, fmt.Errorf("mdblist %s: %w", path, ue.Err)
	}
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return nil, errNotFound
	default:
		// The path, never the address: the key rides in it.
		return nil, fmt.Errorf("mdblist %s: %s", path, resp.Status)
	}
	var body struct {
		Ratings []struct {
			Source string   `json:"source"`
			Score  *float64 `json:"score"`
			Votes  *int     `json:"votes"`
		} `json:"ratings"`
	}
	if err := provider.Decode(resp.Body, &body); err != nil {
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
