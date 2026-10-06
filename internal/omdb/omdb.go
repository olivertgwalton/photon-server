// Package omdb reads The Open Movie Database, which knows a title by its IMDb id: its name, plot,
// certificate, genres and poster, its episodes, and IMDb's and Rotten Tomatoes' scores, as
// Jellyfin's OMDb provider reads them. Each server uses its own key, set by an admin.
package omdb

import (
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
	"github.com/olivertgwalton/photon-server/internal/provider"
)

const baseURL = "https://www.omdbapi.com/"

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
	return &Client{base: baseURL, settings: settings, api: provider.Client{Name: "omdb", Limits: limits, Limit: limit}}
}

func (c *Client) Info() provider.Info {
	return provider.Info{
		ID: domain.SourceOMDb, Name: "The Open Movie Database", Kinds: []domain.ItemKind{domain.ItemMovie, domain.ItemShow},
		Settings: []provider.Setting{{Key: keySetting, Name: "API key", Secret: true, Required: true}},
	}
}

// Match answers the title's IMDb id: OMDb is asked by nothing else, as Jellyfin asks it, so a title
// with none is not matched.
func (c *Client) Match(_ context.Context, _ domain.ItemKind, h provider.Hints) (string, error) {
	return h.IDs[domain.ProviderIMDb], nil
}

// title is OMDb's answer for a film, a show or an episode. A field it has nothing for is "N/A".
type title struct {
	Title      string `json:"Title"`
	Year       string `json:"Year"`
	Rated      string `json:"Rated"`
	Released   string `json:"Released"`
	Genre      string `json:"Genre"`
	Plot       string `json:"Plot"`
	Poster     string `json:"Poster"`
	IMDbRating string `json:"imdbRating"`
	IMDbVotes  string `json:"imdbVotes"`
	Ratings    []struct {
		Source string `json:"Source"`
		Value  string `json:"Value"`
	} `json:"Ratings"`
}

type season struct {
	Episodes []struct {
		Title    string `json:"Title"`
		Released string `json:"Released"`
		Episode  string `json:"Episode"`
		IMDbID   string `json:"imdbID"`
	} `json:"Episodes"`
}

// Describe answers what OMDb says of a title and, for a show numbered as aired, of the seasons
// asked for. A title OMDb does not know is described with nothing.
func (c *Client) Describe(ctx context.Context, kind domain.ItemKind, id string, seasons domain.SeasonRequest) (domain.Metadata, map[int]domain.SeasonMetadata, error) {
	set, err := c.settings(ctx)
	if err != nil {
		return domain.Metadata{}, nil, err
	}
	key := set[keySetting]
	if key == "" {
		return domain.Metadata{}, nil, provider.ErrNotConfigured
	}
	said := map[int]domain.SeasonMetadata{}
	var t title
	found, err := c.get(ctx, key, url.Values{"i": {id}, "plot": {"full"}}, &t)
	if err != nil || !found {
		return domain.Metadata{}, said, err
	}
	m := t.metadata()
	m.IDs = map[domain.Provider]string{domain.ProviderIMDb: id}
	m.Ratings = t.ratings()
	if poster := value(t.Poster); poster != "" {
		m.Artwork = []domain.Artwork{{Kind: domain.ArtworkPoster, URL: poster}}
	}
	// OMDb numbers episodes as IMDb does, as aired.
	if kind != domain.ItemShow || seasons.Order != domain.OrderAired {
		return m, said, nil
	}
	for _, number := range seasons.Numbers {
		var s season
		found, err := c.get(ctx, key, url.Values{"i": {id}, "Season": {strconv.Itoa(number)}}, &s)
		if err != nil {
			return domain.Metadata{}, nil, err
		}
		if !found {
			continue
		}
		episodes := make(map[int]domain.Metadata, len(s.Episodes))
		for _, e := range s.Episodes {
			n, err := strconv.Atoi(e.Episode)
			if err != nil {
				continue
			}
			aired, _ := time.Parse(time.DateOnly, e.Released)
			episode := domain.Metadata{Title: value(e.Title), ReleaseDate: aired, Year: provider.Year(aired)}
			// A season lists no plots: each episode is asked for its own, as Jellyfin asks.
			if value(e.IMDbID) != "" {
				var full title
				found, err := c.get(ctx, key, url.Values{"i": {e.IMDbID}, "plot": {"full"}}, &full)
				if err != nil {
					return domain.Metadata{}, nil, err
				}
				if found {
					episode.Overview = value(full.Plot)
				}
			}
			episodes[n] = episode
		}
		said[number] = domain.SeasonMetadata{Episodes: episodes}
	}
	return m, said, nil
}

func (t title) metadata() domain.Metadata {
	released, _ := time.Parse("02 Jan 2006", t.Released)
	// A show's year is its run, "2008–2013".
	y, _ := strconv.Atoi(t.Year[:min(len(t.Year), 4)])
	out := domain.Metadata{
		Title: value(t.Title), Overview: value(t.Plot), Certificate: value(t.Rated),
		ReleaseDate: released, Year: y,
	}
	if genres := value(t.Genre); genres != "" {
		out.Genres = strings.Split(genres, ", ")
	}
	return out
}

// ratings answers IMDb's score and the Tomatometer, out of 100. OMDb's other sites, Metacritic's,
// are not kept.
func (t title) ratings() []domain.Rating {
	var out []domain.Rating
	if score, err := strconv.ParseFloat(t.IMDbRating, 64); err == nil {
		votes, _ := strconv.Atoi(strings.ReplaceAll(t.IMDbVotes, ",", ""))
		out = append(out, domain.Rating{Site: domain.SiteIMDb, Score: score * 10, Votes: votes})
	}
	for _, r := range t.Ratings {
		if r.Source != "Rotten Tomatoes" {
			continue
		}
		if score, err := strconv.ParseFloat(strings.TrimSuffix(r.Value, "%"), 64); err == nil {
			out = append(out, domain.Rating{Site: domain.SiteRottenTomatoes, Score: min(score, 100)})
		}
	}
	return out
}

// answer is what OMDb says of a request beside its answer: "False", with why, for a title or season
// it does not know, and for a key it refuses.
type answer struct {
	Response string `json:"Response"`
	Error    string `json:"Error"`
}

// get asks OMDb for one answer into into, answering false for a title it does not know. A key it
// refuses or one past its daily allowance is provider.ErrUnavailable, so the title is described by
// its other sources until the key is mended or the day is out.
func (c *Client) get(ctx context.Context, key string, q url.Values, into any) (bool, error) {
	q.Set("apikey", key)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"?"+q.Encode(), nil)
	if err != nil {
		return false, err
	}
	var raw json.RawMessage
	if err := c.api.Do(req, &raw); err != nil {
		if r, ok := errors.AsType[*provider.Refusal](err); ok && r.Code == http.StatusUnauthorized {
			var a answer
			_ = json.Unmarshal(r.Body, &a)
			return false, fmt.Errorf("%w: %s: %w", provider.ErrUnavailable, a.Error, err)
		}
		return false, err
	}
	var a answer
	if err := json.Unmarshal(raw, &a); err != nil {
		return false, err
	}
	if a.Response == "False" {
		return false, nil
	}
	return true, json.Unmarshal(raw, into)
}

func value(s string) string {
	if s == "N/A" {
		return ""
	}
	return s
}
