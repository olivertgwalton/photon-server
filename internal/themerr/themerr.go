// Package themerr fetches films' and shows' theme tunes as Jellyfin's Themerr plugin does: the
// YouTube link ThemerrDB lists for a title's TMDB id, or a film's IMDb id, its sound fetched with
// yt-dlp.
package themerr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/jobs"
	"github.com/olivertgwalton/photon-server/internal/kv"
	"github.com/olivertgwalton/photon-server/internal/media"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// DB is where ThemerrDB publishes its entries, each at {type}/{site}/{id}.json.
const DB = "https://app.lizardbyte.dev/ThemerrDB/"

var (
	// dbLimit keeps every node together gentle with a host that publishes no limit.
	dbLimit = kv.Limit{Every: time.Second, Burst: 5}
	// youtubeLimit keeps every node together from fetching like a bot, which YouTube stops.
	youtubeLimit = kv.Limit{Every: 10 * time.Second, Burst: 1}
)

const (
	// unlistedFor is how long a title ThemerrDB has no theme for goes unasked, as Themerr keeps
	// ThemerrDB's answers a day.
	unlistedFor = 24 * time.Hour
	// refusedFor is how long a link YouTube would not give goes untried. ThemerrDB replaces a dead
	// link with another, which is tried at once.
	refusedFor = 7 * 24 * time.Hour
	// maxSize bounds a tune; ThemerrDB's run to a few minutes, a few MiB.
	maxSize = "32M"
	// maxEntry bounds a ThemerrDB entry, a few KiB of TMDB's details.
	maxEntry = 1 << 20
)

type subjects interface {
	ThemeSubject(ctx context.Context, id uuid.UUID) (store.ThemeSubject, bool, error)
	SaveFetchedTheme(ctx context.Context, item, id uuid.UUID, url string) error
}

type cache interface {
	KeepSound(id uuid.UUID, r io.Reader) error
	Kept(id uuid.UUID) (*os.File, error)
}

type misses interface {
	kv.Limiter
	NoteThemeMissing(ctx context.Context, key string, ttl time.Duration) error
	ThemeMissing(ctx context.Context, key string) (bool, error)
}

// Fetch keeps a title's theme from the link ThemerrDB lists in the cache, with yt-dlp, which uses
// ffmpeg to keep it as AAC in MP4, as browsers and AVPlayer both play. A link fetched before is not
// fetched again; a title ThemerrDB does not list, or a link YouTube refuses, is an answer of no
// theme, noted for a while rather than failed.
func Fetch(st subjects, c cache, m misses, db, ytdlp, ffmpeg string, logger *slog.Logger) jobs.Handler {
	client := &http.Client{Timeout: 30 * time.Second}
	return func(ctx context.Context, item uuid.UUID) error {
		s, ok, err := st.ThemeSubject(ctx, item)
		if err != nil || !ok {
			return err
		}
		if unlisted, err := m.ThemeMissing(ctx, item.String()); err != nil || unlisted {
			return err
		}
		link, err := lookUp(ctx, client, m, db, s)
		if err != nil {
			return err
		}
		if link == "" {
			return m.NoteThemeMissing(ctx, item.String(), unlistedFor)
		}
		if link == s.URL {
			if f, err := c.Kept(s.Theme); err == nil {
				return f.Close()
			}
		}
		if refused, err := m.ThemeMissing(ctx, link); err != nil || refused {
			return err
		}
		if err := kv.Wait(ctx, m, "youtube", youtubeLimit); err != nil {
			return err
		}
		id := uuid.NewV7()
		err = download(ctx, ytdlp, ffmpeg, link, func(r io.Reader) error { return c.KeepSound(id, r) })
		if errors.Is(err, errRefused) {
			logger.WarnContext(ctx, "no theme from youtube", slog.String("item", item.String()), slog.String("url", link), slog.Any("err", err))
			return m.NoteThemeMissing(ctx, link, refusedFor)
		}
		if err != nil {
			return err
		}
		return st.SaveFetchedTheme(ctx, item, id, link)
	}
}

// lookUp answers the YouTube link ThemerrDB lists for s, by its TMDB id, then a film's IMDb id,
// as ThemerrDB keeps films under both and shows under TMDB's alone; "" where it lists none.
func lookUp(ctx context.Context, client *http.Client, l kv.Limiter, db string, s store.ThemeSubject) (string, error) {
	kind, keys := "movies", []string{"themoviedb/" + s.TMDB, "imdb/" + s.IMDb}
	if s.Kind == domain.ItemShow {
		kind, keys = "tv_shows", keys[:1]
	}
	for _, key := range keys {
		if strings.HasSuffix(key, "/") {
			continue
		}
		if err := kv.Wait(ctx, l, "themerrdb", dbLimit); err != nil {
			return "", err
		}
		link, err := entry(ctx, client, db+kind+"/"+key+".json")
		if link != "" || err != nil {
			return link, err
		}
	}
	return "", nil
}

func entry(ctx context.Context, client *http.Client, address string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return "", nil
	default:
		return "", fmt.Errorf("%s: %s", address, resp.Status)
	}
	var e struct {
		Link string `json:"youtube_theme_url"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxEntry)).Decode(&e); err != nil {
		return "", fmt.Errorf("%s: %w", address, err)
	}
	if u, err := url.Parse(e.Link); err != nil || u.Scheme != "https" {
		return "", nil
	}
	return e.Link, nil
}

// errRefused is YouTube declining to give a video: removed, private, blocked by its rights holder,
// behind an age gate, or too long to be a theme.
var errRefused = errors.New("youtube refused the video")

// download fetches link's sound with yt-dlp and hands it to keep. yt-dlp's YouTube extractor alone
// is used, so a link ThemerrDB lists elsewhere is fetched from nowhere.
func download(ctx context.Context, ytdlp, ffmpeg, link string, keep func(io.Reader) error) error {
	dir, err := os.MkdirTemp("", "themerr")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	ctx, cancel := media.Within(ctx, ytdlp, media.PartRun)
	defer cancel()
	c := media.NewCommand(ctx, media.Background, nil, ytdlp,
		"--ignore-config", "--no-cache-dir", "--no-playlist", "--quiet", "--no-warnings",
		"--use-extractors", "youtube", "--max-filesize", maxSize,
		"--format", "bestaudio[ext=m4a]/bestaudio", "--extract-audio", "--audio-format", "m4a",
		"--ffmpeg-location", ffmpeg, "--paths", dir, "--output", "theme.%(ext)s", "--", link)
	if err := c.Err(c.Run()); err != nil {
		if refused(err) {
			return fmt.Errorf("%w: %w", errRefused, err)
		}
		return err
	}
	f, err := os.Open(filepath.Join(dir, "theme.m4a"))
	if errors.Is(err, fs.ErrNotExist) {
		// yt-dlp skips a file over --max-filesize, and says nothing under --quiet.
		return fmt.Errorf("%w: over %s", errRefused, maxSize)
	}
	if err != nil {
		return err
	}
	defer f.Close()
	return keep(f)
}

// refused is whether yt-dlp's failure is YouTube's answer about the video, which it writes as
// "ERROR: [youtube] {id}: {why}", rather than a page or the video's data it could not fetch,
// which may answer later.
func refused(err error) bool {
	said := err.Error()
	return strings.Contains(said, "ERROR: [youtube] ") && !strings.Contains(said, "Unable to download")
}
