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
	"time"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/provider"
)

const baseURL = "https://lrclib.net/api/"

// near is how far a recording LRCLIB found may be from the track's length and still be the track,
// as LRCLIB's own exact lookup allows.
const near = 2

// gap is how long LRCLIB is let be between one answer and the next request. Its documentation asks a
// client fetching a whole library's lyrics to send one request at a time, 200 to 500ms apart, and
// a scan fetches every track's.
const gap = 500 * time.Millisecond

// appID is photon-server's application registered with LRCLIB, sent with every request for its
// higher allowance. It is a client-side application's ID, which LRCLIB says is public and safe to
// ship, so every server sends the same.
const appID = "wxsfG0FcBhGKgjvVBfYCwbko8hZ3MKBHWX3qXF3D_HE"

// longest is the longest duration LRCLIB takes, in seconds; it refuses a lookup by any longer.
const longest = 3600

type Client struct {
	api    provider.Client
	header http.Header
	// turn is held for a request and the gap after it, so requests go one at a time.
	turn chan struct{}
	gap  time.Duration
}

// New is a client naming the server, at version, and where it is from, as LRCLIB asks of every
// client: "LRCGET v0.2.0 (https://github.com/tranxuanthang/lrcget)".
func New(version string, pauses provider.Pauses) *Client {
	return &Client{
		api: provider.Client{Name: "lrclib", Base: baseURL, Pauses: pauses},
		header: http.Header{
			"User-Agent":    {"photon-server " + version + " (https://github.com/olivertgwalton/photon-server)"},
			"Lrclib-App-Id": {appID},
		},
		turn: make(chan struct{}, 1),
		gap:  gap,
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
	if whole := int(math.Round(seconds)); whole >= 1 && whole <= longest {
		exact.Set("duration", strconv.Itoa(whole))
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
	select {
	case c.turn <- struct{}{}:
	case <-ctx.Done():
		return false, ctx.Err()
	}
	defer func() {
		select {
		case <-ctx.Done():
		case <-time.After(c.gap):
		}
		<-c.turn
	}()
	err := c.api.Do(ctx, provider.Request{Method: http.MethodGet, Path: path, Query: q, Header: c.header}, into)
	if errors.Is(err, provider.ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}
