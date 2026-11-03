// Package musicbrainz reads MusicBrainz, which describes an artist and an album, its release group,
// by MBID, and the Cover Art Archive, which keeps an album's cover, as Jellyfin's MusicBrainz
// provider reads them. It needs no key, but asks to be told who is asking, and answers one request a
// second from an address.
package musicbrainz

import (
	"cmp"
	"context"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/provider"
)

const (
	baseURL     = "https://musicbrainz.org/ws/2/"
	coverArtURL = "https://coverartarchive.org/"
)

// every is how often MusicBrainz is asked, its limit for an address.
const every = time.Second

// confident is the least score of MusicBrainz's search a match is taken at.
const confident = 90

type Client struct {
	api, covers provider.Client
	header      http.Header
	// mu holds each request back until every has passed since the last.
	mu   sync.Mutex
	last time.Time
}

// New is a client naming the server, at version, as MusicBrainz asks every client to name itself.
func New(version string, pauses provider.Pauses) *Client {
	return &Client{
		api:    provider.Client{Name: "musicbrainz", Base: baseURL, Pauses: pauses},
		covers: provider.Client{Name: "coverartarchive", Base: coverArtURL},
		header: http.Header{
			"User-Agent": {"photon-server/" + version + " ( https://github.com/olivertgwalton/photon-server )"},
			"Accept":     {"application/json"},
		},
	}
}

func (c *Client) Info() provider.Info {
	return provider.Info{ID: domain.SourceMusicBrainz, Name: "MusicBrainz", Kinds: []domain.ItemKind{domain.ItemArtist, domain.ItemAlbum}}
}

// credit is one artist an album credits.
type credit struct {
	Name string `json:"name"`
}

type releaseGroup struct {
	ID           string   `json:"id"`
	Score        int      `json:"score"`
	Title        string   `json:"title"`
	FirstRelease string   `json:"first-release-date"`
	Credits      []credit `json:"artist-credit"`
	Genres       []genre  `json:"genres"`
}

type artist struct {
	ID             string  `json:"id"`
	Score          int     `json:"score"`
	Name           string  `json:"name"`
	SortName       string  `json:"sort-name"`
	Disambiguation string  `json:"disambiguation"`
	Genres         []genre `json:"genres"`
}

type genre struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// Match answers an artist's or an album's MBID: the one it carries, else the one MusicBrainz's
// search is confident of by its name, and an album's artist and year where they are known. An
// artist two of whose namesakes MusicBrainz is as sure of is no confident match.
func (c *Client) Match(ctx context.Context, _ domain.Locale, kind domain.ItemKind, h provider.Hints) (string, error) {
	if id := h.IDs[domain.ProviderMusicBrainz]; id != "" {
		return id, nil
	}
	switch kind {
	case domain.ItemAlbum:
		query := "releasegroup:" + quoted(h.Title)
		if h.Artist != "" {
			query += " AND artist:" + quoted(h.Artist)
		}
		var found struct {
			Groups []releaseGroup `json:"release-groups"`
		}
		if err := c.get(ctx, &c.api, "release-group/", url.Values{"query": {query}, "limit": {"5"}}, &found); err != nil {
			return "", err
		}
		for _, g := range found.Groups {
			if g.Score >= confident && strings.EqualFold(g.Title, h.Title) && (h.Year == 0 || abs(yearOf(g.FirstRelease)-h.Year) <= 1) {
				return g.ID, nil
			}
		}
	case domain.ItemArtist:
		var found struct {
			Artists []artist `json:"artists"`
		}
		if err := c.get(ctx, &c.api, "artist/", url.Values{"query": {"artist:" + quoted(h.Title)}, "limit": {"5"}}, &found); err != nil {
			return "", err
		}
		named := slices.DeleteFunc(found.Artists, func(a artist) bool { return a.Score < confident || !strings.EqualFold(a.Name, h.Title) })
		if len(named) == 1 {
			return named[0].ID, nil
		}
	case domain.ItemMovie, domain.ItemShow, domain.ItemSeason, domain.ItemEpisode, domain.ItemExtra, domain.ItemCollection, domain.ItemTrack:
	}
	return "", nil
}

// Describe answers what MusicBrainz says of an artist (its sort name and genres) or of an album
// (its title, first release and genres, and the Cover Art Archive's cover where it keeps one).
func (c *Client) Describe(ctx context.Context, _ domain.Locale, kind domain.ItemKind, id string, _ domain.SeasonRequest) (domain.Metadata, map[int]domain.SeasonMetadata, error) {
	m := domain.Metadata{IDs: map[domain.Provider]string{domain.ProviderMusicBrainz: id}}
	switch kind {
	case domain.ItemAlbum:
		var g releaseGroup
		if err := c.get(ctx, &c.api, "release-group/"+url.PathEscape(id), url.Values{"inc": {"genres"}}, &g); err != nil {
			return domain.Metadata{}, nil, err
		}
		m.Title, m.Genres = g.Title, genres(g.Genres)
		m.ReleaseDate, m.Year = released(g.FirstRelease)
		cover, err := c.cover(ctx, id)
		if err != nil {
			return domain.Metadata{}, nil, err
		}
		if cover != "" {
			m.Artwork = []domain.Artwork{{Kind: domain.ArtworkPoster, URL: cover}}
		}
	case domain.ItemArtist:
		var a artist
		if err := c.get(ctx, &c.api, "artist/"+url.PathEscape(id), url.Values{"inc": {"genres"}}, &a); err != nil {
			return domain.Metadata{}, nil, err
		}
		m.Title, m.SortTitle, m.Genres = a.Name, a.SortName, genres(a.Genres)
	case domain.ItemMovie, domain.ItemShow, domain.ItemSeason, domain.ItemEpisode, domain.ItemExtra, domain.ItemCollection, domain.ItemTrack:
	}
	return m, nil, nil
}

// cover is the address of an album's front cover in the Cover Art Archive, at its 1200-pixel size
// where it has one; "" where it keeps none.
func (c *Client) cover(ctx context.Context, id string) (string, error) {
	var art struct {
		Images []struct {
			Front      bool              `json:"front"`
			Image      string            `json:"image"`
			Thumbnails map[string]string `json:"thumbnails"`
		} `json:"images"`
	}
	err := c.covers.Do(ctx, provider.Request{Method: http.MethodGet, Path: "release-group/" + url.PathEscape(id), Header: c.header}, &art)
	if errors.Is(err, provider.ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	for _, im := range art.Images {
		if im.Front {
			return cmp.Or(im.Thumbnails["1200"], im.Image), nil
		}
	}
	return "", nil
}

// get asks MusicBrainz for JSON, a second after it was last asked.
func (c *Client) get(ctx context.Context, api *provider.Client, path string, q url.Values, into any) error {
	if err := c.wait(ctx); err != nil {
		return err
	}
	q.Set("fmt", "json")
	return api.Do(ctx, provider.Request{Method: http.MethodGet, Path: path, Query: q, Header: c.header}, into)
}

func (c *Client) wait(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if d := time.Until(c.last.Add(every)); d > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(d):
		}
	}
	c.last = time.Now()
	return nil
}

// quoted is a name as a phrase of MusicBrainz's Lucene search, its quotes and backslashes escaped.
func quoted(name string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(name) + `"`
}

// genres are MusicBrainz's genres, the most voted for first, as many as a title page shows.
func genres(gs []genre) []string {
	slices.SortStableFunc(gs, func(a, b genre) int { return cmp.Compare(b.Count, a.Count) })
	var out []string
	for _, g := range gs[:min(len(gs), 5)] {
		out = append(out, g.Name)
	}
	return out
}

// released reads MusicBrainz's date, "2025-01-31", "2025-01" or "2025", as the day it names, if it
// names one, and its year.
func released(s string) (time.Time, int) {
	if t, err := time.Parse(time.DateOnly, s); err == nil {
		return t, t.Year()
	}
	return time.Time{}, yearOf(s)
}

func yearOf(s string) int {
	if len(s) < 4 {
		return 0
	}
	year, err := strconv.Atoi(s[:4])
	if err != nil {
		return 0
	}
	return year
}

func abs(n int) int { return max(n, -n) }
