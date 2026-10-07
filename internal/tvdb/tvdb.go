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
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/text/language"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/kv"
	"github.com/olivertgwalton/photon-server/internal/provider"
)

const baseURL = "https://api4.thetvdb.com/v4"

// DefaultKey is the project's own API key, shipped in the source as Jellyfin's plugin ships its
// key. TVDB's terms make it free for a project under its revenue threshold, with attribution.
const DefaultKey = "cdff72c4-d3d6-4ae5-99e6-cae67aff0646"

// TVDB publishes no rate limit; this keeps every node together well within reason.
var limit = kv.Limit{Every: 50 * time.Millisecond, Burst: 20}

// A token lasts a month; it is renewed a few days early.
const tokenLife = 25 * 24 * time.Hour

type Client struct {
	base string
	key  string
	pin  string
	api  provider.Client

	mu      sync.Mutex
	token   string
	expires time.Time
}

// New makes a client for a project key and, for a key subscribers pay for, a subscriber's PIN.
func New(key, pin string, limits kv.Limiter) *Client {
	return &Client{base: baseURL, key: key, pin: pin, api: provider.Client{Name: "tvdb", Limits: limits, Limit: limit}}
}

// languageOf is a locale's language as TVDB names it, ISO 639-2.
func languageOf(loc domain.Locale) string {
	base, _ := language.Make(loc.Language).Base()
	return base.ISO3()
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
	if err := c.api.Do(req, &out); err != nil {
		return "", fmt.Errorf("tvdb login: %w", err)
	}
	c.token, c.expires = out.Data.Token, time.Now().Add(tokenLife)
	return c.token, nil
}

func (c *Client) get(ctx context.Context, path string, into any) error {
	for attempt := 0; ; attempt++ {
		token, err := c.login(ctx)
		if err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		err = c.api.Do(req, into)
		if refused, ok := errors.AsType[*provider.Refusal](err); !ok || refused.Code != http.StatusUnauthorized || attempt > 0 {
			return err
		}
		c.mu.Lock()
		c.token = ""
		c.mu.Unlock()
	}
}

// Search answers TVDB's ranking of shows named title, first aired in year where it is not zero.
func (c *Client) Search(ctx context.Context, loc domain.Locale, title string, year int) ([]domain.Candidate, error) {
	lang := languageOf(loc)
	q := url.Values{"query": {title}, "type": {"series"}, "limit": {"10"}}
	if year != 0 {
		q.Set("year", strconv.Itoa(year))
	}
	var out struct {
		Data []struct {
			ID           string            `json:"tvdb_id"`
			Name         string            `json:"name"`
			Year         string            `json:"year"`
			Image        string            `json:"image_url"`
			Overview     string            `json:"overview"`
			Translations map[string]string `json:"translations"`
			Overviews    map[string]string `json:"overviews"`
		} `json:"data"`
	}
	if err := c.get(ctx, "/search?"+q.Encode(), &out); err != nil {
		return nil, err
	}
	found := make([]domain.Candidate, 0, len(out.Data))
	for _, r := range out.Data {
		if _, err := strconv.Atoi(r.ID); err != nil {
			continue
		}
		y, _ := strconv.Atoi(r.Year)
		found = append(found, domain.Candidate{
			ID: r.ID, Title: cmp.Or(r.Translations[lang], r.Name), OriginalTitle: r.Name, Year: y, Poster: r.Image,
			Overview: cmp.Or(r.Overviews[lang], r.Overview),
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
			found = append(found, domain.Candidate{ID: strconv.Itoa(r.Series.ID), Title: r.Series.Name, OriginalTitle: r.Series.Name, Year: y})
		}
	}
	return found, nil
}

type named struct {
	Name string `json:"name"`
}

// character is someone TVDB credits on a show or an episode: an actor as the part they play, or
// one of its crew. Its picture is the person's own, as Jellyfin's plugin takes it, not the part's.
type character struct {
	Role   string `json:"name"`
	Person string `json:"personName"`
	ID     int    `json:"peopleId"`
	Type   string `json:"peopleType"`
	Photo  string `json:"personImgURL"`
	Sort   int    `json:"sort"`
}

// creditKinds are TVDB's people types of those credited; a type left out (a host, a musical
// guest, the crew at large) is not credited.
var creditKinds = map[string]domain.CreditKind{
	"Actor": domain.CreditActor, "Guest Star": domain.CreditGuestStar, "Director": domain.CreditDirector,
	"Writer": domain.CreditWriter, "Producer": domain.CreditProducer, "Executive Producer": domain.CreditProducer,
	"Creator": domain.CreditCreator, "Composer": domain.CreditComposer,
}

// credits are the characters as credits, the cast before the crew as other sources bill them,
// each in TVDB's order, which its list is not in.
func credits(characters []character) []domain.Credit {
	billing := func(c character) int {
		if creditKinds[c.Type].Acting() {
			return 0
		}
		return 1
	}
	slices.SortStableFunc(characters, func(a, b character) int {
		return cmp.Or(cmp.Compare(billing(a), billing(b)), cmp.Compare(a.Sort, b.Sort))
	})
	var out []domain.Credit
	for _, c := range characters {
		kind, ok := creditKinds[c.Type]
		name := strings.TrimSpace(c.Person)
		if !ok || name == "" || c.ID == 0 {
			continue
		}
		cr := domain.Credit{Name: name, IDs: map[domain.Provider]string{domain.ProviderTVDB: strconv.Itoa(c.ID)}, Kind: kind, Role: c.Role}
		if pics := picture(domain.ArtworkPoster, c.Photo); pics != nil {
			cr.Photo = pics[0].URL
		}
		out = append(out, cr)
	}
	return out
}

// sources names the providers TVDB lists among a show's remote ids.
var sources = map[string]domain.Provider{"IMDB": domain.ProviderIMDb, "TheMovieDB.com": domain.ProviderTMDB}

// Details answers what TVDB says about a show in the client's language. A name or write-up TVDB
// has only in other languages is left unsaid, as Jellyfin's plugin leaves it.
func (c *Client) Details(ctx context.Context, loc domain.Locale, id int) (domain.Metadata, error) {
	lang := languageOf(loc)
	var out struct {
		Data struct {
			Name             string      `json:"name"`
			Image            string      `json:"image"`
			FirstAired       string      `json:"firstAired"`
			Characters       []character `json:"characters"`
			Artworks         []artwork   `json:"artworks"`
			Genres           []named     `json:"genres"`
			OriginalNetwork  *named      `json:"originalNetwork"`
			LatestNetwork    *named      `json:"latestNetwork"`
			OriginalLanguage string      `json:"originalLanguage"`
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
	// Not short: a short record leaves out the cast, with the artwork and trailers.
	if err := c.get(ctx, fmt.Sprintf("/series/%d/extended?meta=translations", id), &out); err != nil {
		return domain.Metadata{}, err
	}
	d := out.Data
	aired := provider.Date(d.FirstAired)
	// A show listing no artworks has its own poster still.
	if len(d.Artworks) == 0 {
		d.Artworks = []artwork{{Type: 2, Image: d.Image}}
	}
	m := domain.Metadata{
		ReleaseDate: aired, Year: provider.Year(aired),
		IDs:     map[domain.Provider]string{domain.ProviderTVDB: strconv.Itoa(id)},
		Artwork: artworks(d.Artworks, lang),
		Credits: credits(d.Characters),
	}
	if d.OriginalLanguage == lang {
		m.OriginalTitle = d.Name
	}
	for _, t := range d.Translations.Names {
		if t.Language == lang && !t.IsAlias {
			m.Title = cmp.Or(m.Title, t.Name)
		}
	}
	for _, t := range d.Translations.Overviews {
		if t.Language == lang {
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
	var rated []provider.Rated
	for _, r := range d.ContentRatings {
		// TVDB names a country by its alpha-3 code.
		if region, err := language.ParseRegion(r.Country); err == nil {
			rated = append(rated, provider.Rated{Country: region.String(), Certificate: r.Name})
		}
	}
	m.Certificate = provider.Certificate(loc.Country, rated)
	for _, r := range d.RemoteIDs {
		if p, ok := sources[r.SourceName]; ok {
			// A TMDB id can arrive as "1438-the-wire".
			v, _, _ := strings.Cut(r.ID, "-")
			m.IDs[p] = v
		}
	}
	return m, nil
}

// seasonTypes are TheTVDB's names for its episode orders.
var seasonTypes = map[domain.EpisodeOrder]string{domain.OrderAired: "default", domain.OrderDVD: "dvd", domain.OrderAbsolute: "absolute"}

// Seasons answers what TVDB says about the episodes of the given seasons, numbered in order.
func (c *Client) Seasons(ctx context.Context, loc domain.Locale, id int, seasons []int, order domain.EpisodeOrder) (map[int]domain.SeasonMetadata, error) {
	lang := languageOf(loc)
	out := map[int]domain.SeasonMetadata{}
	for _, n := range seasons {
		out[n] = domain.SeasonMetadata{Episodes: map[int]domain.Metadata{}}
	}
	path := fmt.Sprintf("/series/%d/episodes/%s/%s?page=0", id, cmp.Or(seasonTypes[order], "default"), lang)
	for path != "" {
		var page struct {
			Data struct {
				Episodes []struct {
					ID       int    `json:"id"`
					Season   int    `json:"seasonNumber"`
					Number   int    `json:"number"`
					Name     string `json:"name"`
					Overview string `json:"overview"`
					Aired    string `json:"aired"`
					Image    string `json:"image"`
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
				// An episode's guests and crew are on its own record alone, as Jellyfin's plugin
				// reads them: one request an episode, of the seasons asked about only.
				people, err := c.episodeCredits(ctx, e.ID)
				if err != nil && !errors.Is(err, provider.ErrNotFound) {
					return nil, err
				}
				aired := provider.Date(e.Aired)
				s.Episodes[e.Number] = domain.Metadata{
					Title: e.Name, Overview: e.Overview, ReleaseDate: aired, Year: provider.Year(aired),
					Artwork: picture(domain.ArtworkThumb, e.Image), Credits: people,
				}
			}
		}
		path = strings.TrimPrefix(page.Links.Next, c.base)
	}
	return out, nil
}

// episodeCredits answers who TVDB credits on an episode: its guests, and crew such as its
// director and writers.
func (c *Client) episodeCredits(ctx context.Context, id int) ([]domain.Credit, error) {
	var out struct {
		Data struct {
			Characters []character `json:"characters"`
		} `json:"data"`
	}
	if err := c.get(ctx, fmt.Sprintf("/episodes/%d/extended", id), &out); err != nil {
		return nil, err
	}
	return credits(out.Data.Characters), nil
}

// artwork is one of a show's pictures, by TVDB's type: its language ISO 639-2, none for one with
// no words on it.
type artwork struct {
	Type     int     `json:"type"`
	Image    string  `json:"image"`
	Language string  `json:"language"`
	Score    float64 `json:"score"`
	Width    int     `json:"width"`
	Height   int     `json:"height"`
}

// artworkKinds are TVDB's types of a show's pictures, as its /artwork/types lists them.
var artworkKinds = map[int]domain.ArtworkKind{
	1: domain.ArtworkBanner, 2: domain.ArtworkPoster, 3: domain.ArtworkBackdrop, 23: domain.ArtworkLogo,
}

// artworks are a show's pictures of each kind, ranked as TMDB's are: a poster, logo or banner in
// lang, else English, else wordless; a backdrop wordless first, as Jellyfin ranks them.
func artworks(all []artwork, lang string) []domain.Artwork {
	var out []domain.Artwork
	for _, kind := range []domain.ArtworkKind{domain.ArtworkPoster, domain.ArtworkBackdrop, domain.ArtworkLogo, domain.ArtworkBanner} {
		of := slices.DeleteFunc(slices.Clone(all), func(a artwork) bool { return artworkKinds[a.Type] != kind || a.Image == "" })
		preferred := []string{lang, "eng", ""}
		if kind == domain.ArtworkBackdrop {
			preferred = []string{"", lang, "eng"}
		}
		ranked := provider.Preferred(of, func(a artwork) string { return a.Language }, func(a, b artwork) int {
			return cmp.Compare(b.Score, a.Score)
		}, preferred...)
		for _, a := range ranked {
			pics := picture(kind, a.Image)
			if pics == nil {
				continue
			}
			pic := pics[0]
			pic.Width, pic.Height = a.Width, a.Height
			// Kept as TMDB names a language, ISO 639-1.
			if base, err := language.ParseBase(a.Language); err == nil {
				pic.Language = base.String()
			}
			out = append(out, pic)
		}
	}
	return out
}

// picture is TVDB's picture at address, which it answers as its bare /banners/ folder for a
// record with none, and as a path for older records.
func picture(kind domain.ArtworkKind, address string) []domain.Artwork {
	if address == "" || strings.HasSuffix(address, "/banners/") {
		return nil
	}
	if strings.HasPrefix(address, "/") {
		address = "https://artworks.thetvdb.com" + address
	}
	return []domain.Artwork{{Kind: kind, URL: address}}
}
