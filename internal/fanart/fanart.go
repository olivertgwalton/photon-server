// Package fanart reads fanart.tv, which pictures a film by its TMDB or IMDb id and a show by its
// TheTVDB id: posters, backdrops, logos, banners, and the landscape pictures lettered with the
// title's name that a wide card wears, as Jellyfin's fanart.tv plugin reads them. It describes
// nothing else. Every server asks with photon's own project key; an admin may add a personal key.
package fanart

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/provider"
)

const baseURL = "https://webservice.fanart.tv/v3.2/"

// projectKey is the key fanart.tv issued photon, which every server asks with, as Jellyfin's plugin
// carries its own: fanart.tv means a project's key to ship with it.
const projectKey = "097e6f9e491fdeb5d46b5198dfa28fa2"

// clientSetting holds an admin's personal key, which fanart.tv answers with pictures newer than a
// project key alone sees.
const clientSetting = "client_key"

type Client struct {
	settings provider.Settings
	api      provider.Client
}

func New(settings provider.Settings, pauses provider.Pauses) *Client {
	return &Client{settings: settings, api: provider.Client{Name: "fanart", Base: baseURL, Pauses: pauses}}
}

func (c *Client) Info() provider.Info {
	return provider.Info{
		ID: domain.SourceFanart, Name: "fanart.tv", Kinds: []domain.ItemKind{domain.ItemMovie, domain.ItemShow},
		Settings: []provider.Setting{{Key: clientSetting, Name: "Personal API key", Secret: true}},
	}
}

// Match answers the id fanart.tv knows the title by: a film's TMDB id, else its IMDb id, and a
// show's TheTVDB id. fanart.tv is asked by nothing else, so a title with none is not matched.
func (c *Client) Match(_ context.Context, _ domain.Locale, kind domain.ItemKind, h provider.Hints) (string, error) {
	if kind == domain.ItemMovie {
		return cmp.Or(h.IDs[domain.ProviderTMDB], h.IDs[domain.ProviderIMDb]), nil
	}
	return h.IDs[domain.ProviderTVDB], nil
}

// image is one of fanart.tv's pictures. Its numbers are strings; a picture with no lettering has
// the language "00".
type image struct {
	URL      string `json:"url"`
	Language string `json:"lang"`
	Likes    string `json:"likes"`
	Width    string `json:"width"`
	Height   string `json:"height"`
}

// film and show are fanart.tv's answers, by the names it gives each kind of picture. A logo is HD
// at 800×310 or older at 400×155; the thumb is the 1000×562 landscape picture lettered with the
// title.
type film struct {
	Posters   []image `json:"movieposter"`
	Backdrops []image `json:"moviebackground"`
	HDLogos   []image `json:"hdmovielogo"`
	Logos     []image `json:"movielogo"`
	Thumbs    []image `json:"moviethumb"`
	Banners   []image `json:"moviebanner"`
}

type show struct {
	Posters   []image `json:"tvposter"`
	Backdrops []image `json:"showbackground"`
	HDLogos   []image `json:"hdtvlogo"`
	Logos     []image `json:"clearlogo"`
	Thumbs    []image `json:"tvthumb"`
	Banners   []image `json:"tvbanner"`
}

// Describe answers fanart.tv's pictures of a film or a show, and nothing else of it; a title
// fanart.tv has no pictures of is described with nothing.
func (c *Client) Describe(ctx context.Context, loc domain.Locale, kind domain.ItemKind, id string, _ domain.SeasonRequest) (domain.Metadata, map[int]domain.SeasonMetadata, error) {
	set, err := c.settings(ctx)
	if err != nil {
		return domain.Metadata{}, nil, err
	}
	q := url.Values{"api_key": {projectKey}}
	if set[clientSetting] != "" {
		q.Set(clientSetting, set[clientSetting])
	}
	var kinds []pictured
	switch kind {
	case domain.ItemMovie:
		var f film
		if found, err := c.get(ctx, "movies/"+url.PathEscape(id), q, &f); err != nil || !found {
			return domain.Metadata{}, nil, err
		}
		kinds = []pictured{
			{domain.ArtworkPoster, f.Posters},
			{domain.ArtworkBackdrop, f.Backdrops},
			{domain.ArtworkLogo, append(f.HDLogos, f.Logos...)},
			{domain.ArtworkThumb, f.Thumbs},
			{domain.ArtworkBanner, f.Banners},
		}
	case domain.ItemShow:
		var s show
		if found, err := c.get(ctx, "tv/"+url.PathEscape(id), q, &s); err != nil || !found {
			return domain.Metadata{}, nil, err
		}
		kinds = []pictured{
			{domain.ArtworkPoster, s.Posters},
			{domain.ArtworkBackdrop, s.Backdrops},
			{domain.ArtworkLogo, append(s.HDLogos, s.Logos...)},
			{domain.ArtworkThumb, s.Thumbs},
			{domain.ArtworkBanner, s.Banners},
		}
	case domain.ItemSeason, domain.ItemEpisode, domain.ItemCollection, domain.ItemExtra:
		return domain.Metadata{}, nil, nil
	}
	var m domain.Metadata
	for _, k := range kinds {
		m.Artwork = append(m.Artwork, pictures(loc, k.kind, k.images)...)
	}
	return m, nil, nil
}

type pictured struct {
	kind   domain.ArtworkKind
	images []image
}

// pictures ranks a kind's pictures as TMDB's and TheTVDB's are: lettered in the reader's language,
// else English, else with none; a backdrop with none first. The most liked come first within each,
// and an HD logo before an older one where fanart.tv likes them alike.
func pictures(loc domain.Locale, kind domain.ArtworkKind, images []image) []domain.Artwork {
	preferred := []string{loc.Base(), "en", ""}
	switch {
	case loc.Artwork == domain.ArtworkAny:
		preferred = nil
	case kind == domain.ArtworkBackdrop:
		preferred = []string{"", loc.Base(), "en"}
	}
	language := func(im image) string {
		if im.Language == "00" {
			return ""
		}
		return im.Language
	}
	ranked := provider.Preferred(images, language, func(a, b image) int {
		return cmp.Compare(number(b.Likes), number(a.Likes))
	}, preferred...)
	out := make([]domain.Artwork, 0, len(ranked))
	for _, im := range ranked {
		if im.URL == "" {
			continue
		}
		out = append(out, domain.Artwork{
			Kind: kind, URL: im.URL, Language: language(im), Width: number(im.Width), Height: number(im.Height),
		})
	}
	return out
}

func number(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}

// get asks fanart.tv for a title's pictures, answering false for a title it has none of, which it
// answers 404. A key it refuses is provider.ErrUnavailable, so a title is pictured by its other
// sources meanwhile.
func (c *Client) get(ctx context.Context, path string, q url.Values, into any) (bool, error) {
	err := c.api.Do(ctx, provider.Request{Method: http.MethodGet, Path: path, Query: q}, into)
	if errors.Is(err, provider.ErrNotFound) {
		return false, nil
	}
	if r, ok := errors.AsType[*provider.Refusal](err); ok && (r.Code == http.StatusUnauthorized || r.Code == http.StatusForbidden) {
		return false, fmt.Errorf("%w: %w", provider.ErrUnavailable, err)
	}
	return err == nil, err
}
