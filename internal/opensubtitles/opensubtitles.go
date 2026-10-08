// Package opensubtitles finds and fetches subtitles on OpenSubtitles.com, as Plex's subtitle
// search and Jellyfin's OpenSubtitles plugin do. Each server uses its own consumer key and an
// account to fetch with, set by an admin.
package opensubtitles

import (
	"cmp"
	"context"
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
	"github.com/olivertgwalton/photon-server/internal/provider"
)

const baseURL = "https://api.opensubtitles.com/api/v1"

// filesDomain is where OpenSubtitles serves the files its downloads link to.
const filesDomain = "opensubtitles.com"

// userAgent is how the server names itself, as OpenSubtitles requires of every request.
const userAgent = "Photon v1"

// The settings an admin sets: the consumer key the API is used with, and the account subtitles
// are fetched as.
const (
	keySetting      = "api_key"
	userSetting     = "username"
	passwordSetting = "password"
)

// limit is OpenSubtitles' own: five requests a second.
var limit = kv.Limit{Every: 200 * time.Millisecond, Burst: 5}

// tokenFor is how long a sign-in is used before signing in again; OpenSubtitles' last a day.
const tokenFor = 12 * time.Hour

type Client struct {
	settings provider.Settings
	api      provider.Client

	mu     sync.Mutex
	token  string
	signed time.Time
}

func New(settings provider.Settings, limits kv.Limiter) *Client {
	return &Client{settings: settings, api: provider.Client{Name: "opensubtitles", Base: baseURL, Limits: limits, Limit: limit}}
}

func (c *Client) Info() provider.Info {
	return provider.Info{
		ID: domain.SourceOpenSubtitles, Name: "OpenSubtitles", Kinds: []domain.ItemKind{domain.ItemMovie, domain.ItemShow},
		Settings: []provider.Setting{
			{Key: keySetting, Name: "Consumer API key", Secret: true, Required: true},
			{Key: userSetting, Name: "Username", Required: true},
			{Key: passwordSetting, Name: "Password", Secret: true, Required: true},
		},
	}
}

// request is a call to the API with the consumer key, as the user signed in where token is set.
func request(method, path string, query url.Values, body any, key, token string) provider.Request {
	header := http.Header{"Api-Key": {key}, "User-Agent": {userAgent}, "Content-Type": {"application/json"}}
	if token != "" {
		header.Set("Authorization", "Bearer "+token)
	}
	return provider.Request{Method: method, Path: path, Query: query, Header: header, Body: body}
}

func (c *Client) key(ctx context.Context) (map[string]string, error) {
	set, err := c.settings(ctx)
	if err != nil {
		return nil, err
	}
	if set[keySetting] == "" {
		return nil, provider.ErrNotConfigured
	}
	return set, nil
}

// SearchSubtitles answers OpenSubtitles' subtitles for a title in a language: a film by its IMDb
// or TMDB id, an episode by its show's and its numbers, and the release by its hash, which those
// made for it are marked by. Parameters go sorted, ids bare, as OpenSubtitles asks, so the answer
// is one it keeps.
func (c *Client) SearchSubtitles(ctx context.Context, q domain.SubtitleQuery) ([]domain.FoundSubtitle, error) {
	set, err := c.key(ctx)
	if err != nil {
		return nil, err
	}
	query := url.Values{"languages": {code(q.Language)}}
	ids := ""
	switch q.Kind {
	case domain.ItemEpisode:
		query.Set("type", "episode")
		query.Set("season_number", strconv.Itoa(q.Season))
		query.Set("episode_number", strconv.Itoa(q.Episode))
		ids = "parent_"
	case domain.ItemMovie:
		query.Set("type", "movie")
	case domain.ItemShow, domain.ItemSeason, domain.ItemExtra, domain.ItemCollection:
		return nil, nil
	}
	if imdb := strings.TrimLeft(strings.TrimPrefix(q.IDs[domain.ProviderIMDb], "tt"), "0"); imdb != "" {
		query.Set(ids+"imdb_id", imdb)
	} else if tmdb := q.IDs[domain.ProviderTMDB]; tmdb != "" {
		query.Set(ids+"tmdb_id", tmdb)
	} else {
		return nil, nil
	}
	if q.Hash != "" {
		query.Set("moviehash", q.Hash)
	}
	var body struct {
		Data []struct {
			Attributes struct {
				Language        string `json:"language"`
				Release         string `json:"release"`
				HearingImpaired bool   `json:"hearing_impaired"`
				ForeignOnly     bool   `json:"foreign_parts_only"`
				Downloads       int    `json:"download_count"`
				HashMatch       bool   `json:"moviehash_match"`
				Files           []struct {
					ID int `json:"file_id"`
				} `json:"files"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := c.api.Do(ctx, request(http.MethodGet, "/subtitles", query, nil, set[keySetting], ""), &body); err != nil {
		return nil, err
	}
	var out []domain.FoundSubtitle
	for _, d := range body.Data {
		a := d.Attributes
		// A subtitle in several files is for a release in several parts.
		if len(a.Files) != 1 {
			continue
		}
		lang := language.Make(a.Language)
		out = append(out, domain.FoundSubtitle{
			Source: domain.SourceOpenSubtitles, ID: strconv.Itoa(a.Files[0].ID), Language: lang, Release: a.Release,
			HearingImpaired: a.HearingImpaired, Forced: a.ForeignOnly, ForRelease: a.HashMatch, Downloads: a.Downloads,
		})
	}
	return out, nil
}

// FetchSubtitle fetches a subtitle as SubRip, signed in as the account set, which counts it
// against the account's downloads for the day.
func (c *Client) FetchSubtitle(ctx context.Context, id string) ([]byte, error) {
	set, err := c.key(ctx)
	if err != nil {
		return nil, err
	}
	file, err := strconv.Atoi(id)
	if err != nil {
		return nil, provider.ErrNotFound
	}
	ask := struct {
		FileID int    `json:"file_id"`
		Format string `json:"sub_format"`
	}{file, "srt"}
	var link struct {
		Link string `json:"link"`
	}
	for attempt := range 2 {
		token, err := c.signIn(ctx, set, attempt > 0)
		if err != nil {
			return nil, err
		}
		err = c.api.Do(ctx, request(http.MethodPost, "/download", nil, ask, set[keySetting], token), &link)
		if refusal, ok := errors.AsType[*provider.Refusal](err); ok {
			switch {
			case refusal.Code == http.StatusUnauthorized && attempt == 0:
				continue
			case refusal.Code == http.StatusNotAcceptable:
				return nil, fmt.Errorf("opensubtitles: the account's downloads for today are used: %w", provider.ErrQuota)
			}
		}
		if err != nil {
			return nil, err
		}
		break
	}
	return c.file(ctx, link.Link)
}

// file fetches the file a download links to, from OpenSubtitles alone: a link elsewhere is refused.
func (c *Client) file(ctx context.Context, link string) ([]byte, error) {
	at, err := url.Parse(link)
	if err != nil {
		return nil, fmt.Errorf("opensubtitles: download link: %w", err)
	}
	api, err := url.Parse(c.api.Base)
	if err != nil {
		return nil, err
	}
	host := at.Hostname()
	if at.Scheme != api.Scheme || at.Host != api.Host && host != filesDomain && !strings.HasSuffix(host, "."+filesDomain) {
		return nil, fmt.Errorf("opensubtitles: a download links off OpenSubtitles, to %s", at.Redacted())
	}
	files := c.api
	files.Base = at.Scheme + "://" + at.Host
	return files.Bytes(ctx, provider.Request{Method: http.MethodGet, Path: at.EscapedPath(), Query: at.Query()})
}

// signIn answers a token of the account set, signing in where there is none, it is old, or again
// asks for a new one.
func (c *Client) signIn(ctx context.Context, set map[string]string, again bool) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && !again && time.Since(c.signed) < tokenFor {
		return c.token, nil
	}
	if set[userSetting] == "" || set[passwordSetting] == "" {
		return "", provider.ErrNotConfigured
	}
	login := request(http.MethodPost, "/login", nil,
		map[string]string{"username": set[userSetting], "password": set[passwordSetting]}, set[keySetting], "")
	var body struct {
		Token string `json:"token"`
	}
	if err := c.api.Do(ctx, login, &body); err != nil {
		return "", err
	}
	c.token, c.signed = body.Token, time.Now()
	return c.token, nil
}

// code is OpenSubtitles' name for a language: its two letters, Portuguese and Chinese told apart
// by where they are spoken and written.
func code(tag language.Tag) string {
	base, _ := tag.Base()
	region, _ := tag.Region()
	script, _ := tag.Script()
	switch base.String() {
	case "pt":
		return cmp.Or(map[string]string{"BR": "pt-br"}[region.String()], "pt-pt")
	case "zh":
		if script.String() == "Hant" || region.String() == "TW" || region.String() == "HK" {
			return "zh-tw"
		}
		return "zh-cn"
	}
	return base.String()
}
