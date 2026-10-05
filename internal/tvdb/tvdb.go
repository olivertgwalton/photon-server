// Package tvdb asks TheTVDB about shows.
package tvdb

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/text/language"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/kv"
)

const baseURL = "https://api4.thetvdb.com/v4"

// DefaultKey is the project's own API key, shipped in the source as Jellyfin's plugin ships its
// key. TVDB's terms make it free for a project under its revenue threshold, with attribution.
const DefaultKey = "cdff72c4-d3d6-4ae5-99e6-cae67aff0646"

// TVDB publishes no rate limit; this keeps every node together well within reason.
var limit = kv.Limit{Every: 50 * time.Millisecond, Burst: 20}

// A token lasts a month; it is renewed a few days early.
const tokenLife = 25 * 24 * time.Hour

var ErrNotFound = errors.New("tvdb: not found")

var errExpired = errors.New("tvdb: token refused")

type limiter interface {
	Allow(ctx context.Context, key string, l kv.Limit) (time.Duration, error)
}

type Client struct {
	base     string
	key      string
	pin      string
	language string // ISO 639-2, as TVDB names languages
	country  string // ISO 3166-1 alpha-3, lower case
	http     *http.Client
	limits   limiter

	mu      sync.Mutex
	token   string
	expires time.Time
}

// New makes a client for a project key and, for a key subscribers pay for, a subscriber's PIN.
// language is an IETF tag such as en-GB: TVDB is asked for its language, and its region picks the
// certificates.
func New(key, pin, lang string, limits limiter) *Client {
	tag := language.Make(lang)
	base, _ := tag.Base()
	region, _ := tag.Region()
	return &Client{
		base: baseURL, key: key, pin: pin, language: base.ISO3(), country: strings.ToLower(region.ISO3()),
		http: &http.Client{Timeout: 30 * time.Second}, limits: limits,
	}
}

func (c *Client) login(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && time.Now().Before(c.expires) {
		return c.token, nil
	}
	body, err := json.Marshal(struct { //nolint:gosec // the login request is where the key is meant to go
		Key string `json:"apikey"`
		PIN string `json:"pin,omitzero"`
	}{c.key, c.pin})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/login", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	var out struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := c.do(req, &out); err != nil {
		return "", fmt.Errorf("tvdb login: %w", err)
	}
	c.token, c.expires = out.Data.Token, time.Now().Add(tokenLife)
	return c.token, nil
}

func (c *Client) get(ctx context.Context, path string, into any) error {
	for attempt := 0; ; attempt++ {
		if err := kv.Wait(ctx, c.limits, "tvdb", limit); err != nil {
			return err
		}
		token, err := c.login(ctx)
		if err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		err = c.do(req, into)
		if !errors.Is(err, errExpired) || attempt > 0 {
			return err
		}
		c.mu.Lock()
		c.token = ""
		c.mu.Unlock()
	}
}

func (c *Client) do(req *http.Request, into any) error {
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
	case http.StatusUnauthorized:
		return errExpired
	}
	return fmt.Errorf("tvdb %s: %s", req.URL.Path, resp.Status)
}

// Search answers TVDB's ranking of shows named title, first aired in year where it is not zero.
func (c *Client) Search(ctx context.Context, title string, year int) ([]domain.Candidate, error) {
	q := url.Values{"query": {title}, "type": {"series"}, "limit": {"10"}}
	if year != 0 {
		q.Set("year", strconv.Itoa(year))
	}
	var out struct {
		Data []struct {
			ID           string            `json:"tvdb_id"`
			Name         string            `json:"name"`
			Year         string            `json:"year"`
			Translations map[string]string `json:"translations"`
		} `json:"data"`
	}
	if err := c.get(ctx, "/search?"+q.Encode(), &out); err != nil {
		return nil, err
	}
	found := make([]domain.Candidate, 0, len(out.Data))
	for _, r := range out.Data {
		id, err := strconv.Atoi(r.ID)
		if err != nil {
			continue
		}
		y, _ := strconv.Atoi(r.Year)
		found = append(found, domain.Candidate{
			ID: id, Title: cmp.Or(r.Translations[c.language], r.Name), OriginalTitle: r.Name, Year: y,
		})
	}
	return found, nil
}

// Find answers the shows another provider's id names.
func (c *Client) Find(ctx context.Context, id string) ([]domain.Candidate, error) {
	var out struct {
		Data []struct {
			Series *struct {
				ID   int    `json:"id"`
				Name string `json:"name"`
				Year string `json:"year"`
			} `json:"series"`
		} `json:"data"`
	}
	if err := c.get(ctx, "/search/remoteid/"+url.PathEscape(id), &out); err != nil {
		return nil, err
	}
	var found []domain.Candidate
	for _, r := range out.Data {
		if r.Series != nil {
			y, _ := strconv.Atoi(r.Series.Year)
			found = append(found, domain.Candidate{ID: r.Series.ID, Title: r.Series.Name, OriginalTitle: r.Series.Name, Year: y})
		}
	}
	return found, nil
}

type named struct {
	Name string `json:"name"`
}

// sources names the providers TVDB lists among a show's remote ids.
var sources = map[string]domain.Provider{"IMDB": domain.ProviderIMDb, "TheMovieDB.com": domain.ProviderTMDB}

// Details answers what TVDB says about a show in the client's language. A name or write-up TVDB
// has only in other languages is left unsaid, as Jellyfin's plugin leaves it.
func (c *Client) Details(ctx context.Context, id int) (domain.Metadata, error) {
	var out struct {
		Data struct {
			Name             string  `json:"name"`
			FirstAired       string  `json:"firstAired"`
			Genres           []named `json:"genres"`
			OriginalNetwork  *named  `json:"originalNetwork"`
			LatestNetwork    *named  `json:"latestNetwork"`
			OriginalLanguage string  `json:"originalLanguage"`
			ContentRatings   []struct {
				Name    string `json:"name"`
				Country string `json:"country"`
			} `json:"contentRatings"`
			RemoteIDs []struct {
				ID         string `json:"id"`
				SourceName string `json:"sourceName"`
			} `json:"remoteIds"`
			Translations struct {
				Names []struct {
					Language string `json:"language"`
					Name     string `json:"name"`
					IsAlias  bool   `json:"isAlias"`
				} `json:"nameTranslations"`
				Overviews []struct {
					Language string `json:"language"`
					Overview string `json:"overview"`
				} `json:"overviewTranslations"`
			} `json:"translations"`
		} `json:"data"`
	}
	if err := c.get(ctx, fmt.Sprintf("/series/%d/extended?meta=translations&short=true", id), &out); err != nil {
		return domain.Metadata{}, err
	}
	d := out.Data
	aired := date(d.FirstAired)
	m := domain.Metadata{
		ReleaseDate: aired, Year: year(aired),
		IDs: map[domain.Provider]string{domain.ProviderTVDB: strconv.Itoa(id)},
	}
	if d.OriginalLanguage == c.language {
		m.OriginalTitle = d.Name
	}
	for _, t := range d.Translations.Names {
		if t.Language == c.language && !t.IsAlias {
			m.Title = cmp.Or(m.Title, t.Name)
		}
	}
	for _, t := range d.Translations.Overviews {
		if t.Language == c.language {
			m.Overview = cmp.Or(m.Overview, t.Overview)
		}
	}
	for _, g := range d.Genres {
		m.Genres = append(m.Genres, g.Name)
	}
	for _, n := range []*named{d.OriginalNetwork, d.LatestNetwork} {
		if n != nil && n.Name != "" && (len(m.Studios) == 0 || m.Studios[0] != n.Name) {
			m.Studios = append(m.Studios, n.Name)
		}
	}
	for _, r := range d.ContentRatings {
		if r.Country == c.country {
			m.Certificate = cmp.Or(m.Certificate, r.Name)
		}
	}
	for _, r := range d.RemoteIDs {
		if p, ok := sources[r.SourceName]; ok {
			// A TMDB id can arrive as "1438-the-wire".
			v, _, _ := strings.Cut(r.ID, "-")
			m.IDs[p] = v
		}
	}
	return m, nil
}

// Seasons answers what TVDB says about the episodes of the given seasons, in its aired order.
func (c *Client) Seasons(ctx context.Context, id int, seasons []int) (map[int]domain.SeasonMetadata, error) {
	out := map[int]domain.SeasonMetadata{}
	for _, n := range seasons {
		out[n] = domain.SeasonMetadata{Episodes: map[int]domain.Metadata{}}
	}
	path := fmt.Sprintf("/series/%d/episodes/default/%s?page=0", id, c.language)
	for path != "" {
		var page struct {
			Data struct {
				Episodes []struct {
					Season   int    `json:"seasonNumber"`
					Number   int    `json:"number"`
					Name     string `json:"name"`
					Overview string `json:"overview"`
					Aired    string `json:"aired"`
				} `json:"episodes"`
			} `json:"data"`
			Links struct {
				Next string `json:"next"`
			} `json:"links"`
		}
		if err := c.get(ctx, path, &page); err != nil {
			return nil, err
		}
		for _, e := range page.Data.Episodes {
			if s, ok := out[e.Season]; ok {
				aired := date(e.Aired)
				s.Episodes[e.Number] = domain.Metadata{Title: e.Name, Overview: e.Overview, ReleaseDate: aired, Year: year(aired)}
			}
		}
		path = strings.TrimPrefix(page.Links.Next, c.base)
	}
	return out, nil
}

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
