// Package lrclib reads LRCLIB, an open library of lyrics, many timed line by line, as Jellyfin's
// LrcLib plugin and LRCGET read it. It needs no key, but asks to be told who is asking.
package lrclib

import (
	"context"
	"errors"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/provider"
)

const baseURL = "https://lrclib.net/api/"

// near is how far a recording LRCLIB found may be from the track's length and still be the track,
// as LRCLIB's own exact lookup allows.
const near = 2

type Client struct {
	api    provider.Client
	header http.Header
}

// New is a client naming the server, at version, as LRCLIB asks of every client.
func New(version string, pauses provider.Pauses) *Client {
	return &Client{
		api:    provider.Client{Name: "lrclib", Base: baseURL, Pauses: pauses},
		header: http.Header{"User-Agent": {"photon-server/" + version + " ( https://github.com/olivertgwalton/photon-server )"}},
	}
}

func (c *Client) Info() provider.Info {
	return provider.Info{ID: domain.SourceLRCLIB, Name: "LRCLIB", Kinds: []domain.ItemKind{domain.ItemTrack}}
}

// record is a recording's lyrics as LRCLIB answers them.
type record struct {
	Duration     float64 `json:"duration"`
	Instrumental bool    `json:"instrumental"`
	PlainLyrics  string  `json:"plainLyrics"`
	SyncedLyrics string  `json:"syncedLyrics"`
}

// words are a recording's lyrics, timed where LRCLIB has them timed: "" for an instrumental.
func (r record) words() string {
	if r.Instrumental {
		return ""
	}
	if s := strings.TrimSpace(r.SyncedLyrics); s != "" {
		return s
	}
	return strings.TrimSpace(r.PlainLyrics)
}

// Lyrics are the track's lyrics: the recording of its artist, title, album and length, else the
// first a search of its artist and title finds within a couple of seconds of its length; "" where
// LRCLIB has none.
func (c *Client) Lyrics(ctx context.Context, q domain.LyricsQuery) (string, error) {
	if q.Title == "" || q.Artist == "" {
		return "", nil
	}
	seconds := q.Duration.Seconds()
	exact := url.Values{"artist_name": {q.Artist}, "track_name": {q.Title}}
	if q.Album != "" {
		exact.Set("album_name", q.Album)
	}
	if seconds > 0 {
		exact.Set("duration", strconv.Itoa(int(math.Round(seconds))))
	}
	var r record
	found, err := c.get(ctx, "get", exact, &r)
	if err != nil || found {
		return r.words(), err
	}
	var all []record
	if _, err := c.get(ctx, "search", url.Values{"artist_name": {q.Artist}, "track_name": {q.Title}}, &all); err != nil {
		return "", err
	}
	for _, r := range all {
		if r.words() != "" && (seconds == 0 || math.Abs(r.Duration-seconds) <= near) {
			return r.words(), nil
		}
	}
	return "", nil
}

func (c *Client) get(ctx context.Context, path string, q url.Values, into any) (bool, error) {
	err := c.api.Do(ctx, provider.Request{Method: http.MethodGet, Path: path, Query: q, Header: c.header}, into)
	if errors.Is(err, provider.ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}
