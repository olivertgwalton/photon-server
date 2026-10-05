// Package tmdb asks The Movie Database about films and shows.
package tmdb

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/kv"
)

const baseURL = "https://api.themoviedb.org/3"

// limit keeps every node together well under TMDB's rate limit of about 50 requests a second.
var limit = kv.Limit{Every: 50 * time.Millisecond, Burst: 20}

var ErrNotFound = errors.New("tmdb: not found")

type Kind string

const (
	Movie Kind = "movie"
	Show  Kind = "tv"
)

type limiter interface {
	Allow(ctx context.Context, key string, l kv.Limit) (time.Duration, error)
}

type Client struct {
	base     string
	token    string
	language string
	country  string
	http     *http.Client
	limits   limiter
}

// New makes a client that authenticates with an API read access token and asks for metadata in
// language, an IETF tag such as en-US whose region picks the certificates.
func New(token, language string, limits limiter) *Client {
	_, country, _ := strings.Cut(language, "-")
	return &Client{
		base: baseURL, token: token, language: language, country: strings.ToUpper(country),
		http: &http.Client{Timeout: 30 * time.Second}, limits: limits,
	}
}

func (c *Client) get(ctx context.Context, path string, query url.Values, into any) error {
	for {
		wait, err := c.limits.Allow(ctx, "tmdb", limit)
		if err != nil {
			return err
		}
		if wait == 0 {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
	if query == nil {
		query = url.Values{}
	}
	query.Set("language", c.language)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path+"?"+query.Encode(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		return json.NewDecoder(resp.Body).Decode(into)
	case http.StatusNotFound:
		return ErrNotFound
	}
	return fmt.Errorf("tmdb %s: %s", path, resp.Status)
}

// Match is a search or lookup result.
type Match struct {
	ID            int
	Title         string
	OriginalTitle string
	Year          int
}

type result struct {
	ID            int    `json:"id"`
	Title         string `json:"title"`
	Name          string `json:"name"`
	OriginalTitle string `json:"original_title"`
	OriginalName  string `json:"original_name"`
	ReleaseDate   string `json:"release_date"`
	FirstAirDate  string `json:"first_air_date"`
}

func (r result) match() Match {
	return Match{
		ID: r.ID, Title: cmp.Or(r.Title, r.Name), OriginalTitle: cmp.Or(r.OriginalTitle, r.OriginalName),
		Year: year(date(cmp.Or(r.ReleaseDate, r.FirstAirDate))),
	}
}

func matches(rs []result) []Match {
	out := make([]Match, len(rs))
	for i, r := range rs {
		out[i] = r.match()
	}
	return out
}

// Search answers TMDB's ranking of titles named title, released in year where it is not zero.
func (c *Client) Search(ctx context.Context, kind Kind, title string, year int) ([]Match, error) {
	q := url.Values{"query": {title}, "include_adult": {"false"}}
	if year != 0 {
		q.Set(map[Kind]string{Movie: "year", Show: "first_air_date_year"}[kind], strconv.Itoa(year))
	}
	var page struct {
		Results []result `json:"results"`
	}
	if err := c.get(ctx, "/search/"+string(kind), q, &page); err != nil {
		return nil, err
	}
	return matches(page.Results), nil
}

// Find answers the titles of kind that another provider's id names.
func (c *Client) Find(ctx context.Context, kind Kind, provider domain.Provider, id string) ([]Match, error) {
	source := map[domain.Provider]string{domain.ProviderIMDb: "imdb_id", domain.ProviderTVDB: "tvdb_id"}[provider]
	if source == "" {
		return nil, fmt.Errorf("tmdb: cannot find by %s id", provider)
	}
	var found struct {
		Movies []result `json:"movie_results"`
		Shows  []result `json:"tv_results"`
	}
	if err := c.get(ctx, "/find/"+url.PathEscape(id), url.Values{"external_source": {source}}, &found); err != nil {
		return nil, err
	}
	if kind == Movie {
		return matches(found.Movies), nil
	}
	return matches(found.Shows), nil
}

type named struct {
	Name string `json:"name"`
}

type details struct {
	result
	Overview            string  `json:"overview"`
	Tagline             string  `json:"tagline"`
	Genres              []named `json:"genres"`
	ProductionCompanies []named `json:"production_companies"`
	Networks            []named `json:"networks"`
	ExternalIDs         struct {
		IMDb string `json:"imdb_id"`
		TVDB int    `json:"tvdb_id"`
	} `json:"external_ids"`
	ReleaseDates struct {
		Results []struct {
			Country string `json:"iso_3166_1"`
			Dates   []struct {
				Certification string `json:"certification"`
			} `json:"release_dates"`
		} `json:"results"`
	} `json:"release_dates"`
	ContentRatings struct {
		Results []struct {
			Country string `json:"iso_3166_1"`
			Rating  string `json:"rating"`
		} `json:"results"`
	} `json:"content_ratings"`
}

// Details answers what TMDB says about a title, with its certificate in the client's country.
func (c *Client) Details(ctx context.Context, kind Kind, id int) (domain.Metadata, error) {
	extra := map[Kind]string{Movie: "release_dates,external_ids", Show: "content_ratings,external_ids"}[kind]
	var d details
	if err := c.get(ctx, fmt.Sprintf("/%s/%d", kind, id), url.Values{"append_to_response": {extra}}, &d); err != nil {
		return domain.Metadata{}, err
	}
	m := d.match()
	released := date(cmp.Or(d.ReleaseDate, d.FirstAirDate))
	out := domain.Metadata{
		Title: m.Title, OriginalTitle: m.OriginalTitle, Overview: d.Overview, Tagline: d.Tagline,
		ReleaseDate: released, Year: year(released),
		Genres: names(d.Genres), Studios: names(append(d.ProductionCompanies, d.Networks...)),
		IDs: map[domain.Provider]string{domain.ProviderTMDB: strconv.Itoa(id)},
	}
	if d.ExternalIDs.IMDb != "" {
		out.IDs[domain.ProviderIMDb] = d.ExternalIDs.IMDb
	}
	if d.ExternalIDs.TVDB != 0 {
		out.IDs[domain.ProviderTVDB] = strconv.Itoa(d.ExternalIDs.TVDB)
	}
	for _, r := range d.ReleaseDates.Results {
		for _, rd := range r.Dates {
			if r.Country == c.country {
				out.Certificate = cmp.Or(out.Certificate, rd.Certification)
			}
		}
	}
	for _, r := range d.ContentRatings.Results {
		if r.Country == c.country {
			out.Certificate = cmp.Or(out.Certificate, r.Rating)
		}
	}
	return out, nil
}

func (c *Client) Season(ctx context.Context, show, number int) (domain.SeasonMetadata, error) {
	var s struct {
		Name     string `json:"name"`
		Overview string `json:"overview"`
		AirDate  string `json:"air_date"`
		Episodes []struct {
			Number   int    `json:"episode_number"`
			Name     string `json:"name"`
			Overview string `json:"overview"`
			AirDate  string `json:"air_date"`
		} `json:"episodes"`
	}
	if err := c.get(ctx, fmt.Sprintf("/tv/%d/season/%d", show, number), nil, &s); err != nil {
		return domain.SeasonMetadata{}, err
	}
	aired := date(s.AirDate)
	out := domain.SeasonMetadata{
		Metadata: domain.Metadata{Title: s.Name, Overview: s.Overview, ReleaseDate: aired, Year: year(aired)},
		Episodes: make(map[int]domain.Metadata, len(s.Episodes)),
	}
	for _, e := range s.Episodes {
		aired := date(e.AirDate)
		out.Episodes[e.Number] = domain.Metadata{Title: e.Name, Overview: e.Overview, ReleaseDate: aired, Year: year(aired)}
	}
	return out, nil
}

func names(ns []named) []string {
	out := make([]string, 0, len(ns))
	for _, n := range ns {
		out = append(out, n.Name)
	}
	return out
}

// date reads TMDB's dates; an absent one is the zero time.
func date(s string) time.Time {
	t, _ := time.Parse(time.DateOnly, s)
	return t
}

func year(t time.Time) int {
	if t.IsZero() {
		return 0
	}
	return t.Year()
}
