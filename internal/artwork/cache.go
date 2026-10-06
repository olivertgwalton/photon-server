// Package artwork keeps providers' pictures, and shows' theme tunes, on disk once they have been
// fetched.
package artwork

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"io"
	"io/fs"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"
	"uuid"

	"golang.org/x/sync/singleflight"
)

const (
	// maxPicture bounds what a provider may send for one picture, or a tune; posters run to a few
	// MiB, and the theme host's tunes, half a minute long, to less.
	maxPicture = 32 << 20
	fetchFor   = 30 * time.Second
)

// Cache fetches each picture once, under its id. A picture replaced gets a new id, so a file
// here never goes stale; one replaced is swept away (see Sweep).
type Cache struct {
	root  *os.Root
	http  *http.Client
	group singleflight.Group
	// resizing holds a place for each picture being resized, one per processor.
	resizing chan struct{}
}

func Open(dir string) (*Cache, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	return &Cache{
		root: root, http: &http.Client{Timeout: fetchFor}, resizing: make(chan struct{}, runtime.NumCPU()),
	}, nil
}

func (c *Cache) Close() error { return c.root.Close() }

// File answers the picture with id, fetching it from url the first time it is asked for. Callers
// asking together share one fetch, which carries on if the first of them goes away.
func (c *Cache) File(ctx context.Context, id uuid.UUID, url string) (*os.File, error) {
	return c.file(ctx, id, url, "image/")
}

// Sound answers a theme tune kept under id as File answers a picture: fetched from url the first
// time, ErrMissing where url has none.
func (c *Cache) Sound(ctx context.Context, id uuid.UUID, url string) (*os.File, error) {
	return c.file(ctx, id, url, "audio/")
}

// ErrMissing is an address that answers it has nothing there.
var ErrMissing = errors.New("nothing there")

// file answers the file with id, fetched from url if it is not here, of a type under kind.
func (c *Cache) file(ctx context.Context, id uuid.UUID, url, kind string) (*os.File, error) {
	name := id.String()
	if f, err := c.root.Open(name); !errors.Is(err, fs.ErrNotExist) {
		return f, err
	}
	fetched := c.group.DoChan(name, func() (any, error) {
		return nil, c.fetch(context.WithoutCancel(ctx), name, url, kind)
	})
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case r := <-fetched:
		if r.Err != nil {
			return nil, r.Err
		}
	}
	return c.root.Open(name)
}

func (c *Cache) fetch(ctx context.Context, name, url, kind string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return fmt.Errorf("%s: %w", url, ErrMissing)
	case resp.StatusCode != http.StatusOK:
		return fmt.Errorf("%s: %s", url, resp.Status)
	case !strings.HasPrefix(resp.Header.Get("Content-Type"), kind):
		return fmt.Errorf("%s: %s is not %s", url, resp.Header.Get("Content-Type"), strings.TrimSuffix(kind, "/"))
	}
	return c.write(name, func(w io.Writer) error {
		n, err := io.Copy(w, io.LimitReader(resp.Body, maxPicture+1))
		if err == nil && n > maxPicture {
			err = fmt.Errorf("%s is over %d bytes", url, maxPicture)
		}
		return err
	})
}

// partLife is how long a picture half written may be, before it is taken for one abandoned.
const partLife = time.Hour

// Sweep removes the files of every picture live says is gone, original and resized copies alike,
// and pictures half written long ago. It answers how many files it removed.
func (c *Cache) Sweep(ctx context.Context, live func(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]bool, error)) (int, error) {
	entries, err := fs.ReadDir(c.root.FS(), ".")
	if err != nil {
		return 0, err
	}
	byID := map[uuid.UUID][]string{}
	var stale []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasSuffix(name, ".part") {
			if info, err := e.Info(); err == nil && time.Since(info.ModTime()) > partLife {
				stale = append(stale, name)
			}
			continue
		}
		id, err := uuid.Parse(name[:min(len(name), 36)])
		if err != nil {
			continue
		}
		byID[id] = append(byID[id], name)
	}
	ids := make([]uuid.UUID, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	alive, err := live(ctx, ids)
	if err != nil {
		return 0, err
	}
	for id, names := range byID {
		if !alive[id] {
			stale = append(stale, names...)
		}
	}
	removed := 0
	for _, name := range stale {
		if err := c.root.Remove(name); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return removed, err
		}
		removed++
	}
	return removed, nil
}

// ErrNotPicture is something given to keep that is not a picture decoded here, or too large.
var ErrNotPicture = errors.New("not a picture")

// kept are the formats a picture given to keep may be, by the type its bytes say they are: the
// raster formats decoded here, never SVG, which could carry script.
var kept = map[string]string{"image/jpeg": "jpeg", "image/png": "png", "image/gif": "gif", "image/webp": "webp"}

// Keep keeps a picture given to the server, as an avatar is, under id: at most maxPicture bytes,
// of a format its own bytes say it is, of at most maxPixels.
func (c *Cache) Keep(id uuid.UUID, r io.Reader) error {
	data, err := io.ReadAll(io.LimitReader(r, maxPicture+1))
	if err != nil {
		return err
	}
	if len(data) > maxPicture {
		return fmt.Errorf("%w: over %d MiB", ErrNotPicture, maxPicture>>20)
	}
	format, ok := kept[http.DetectContentType(data)]
	if !ok {
		return fmt.Errorf("%w: a JPEG, PNG, GIF or WebP is kept", ErrNotPicture)
	}
	cfg, decoded, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || decoded != format {
		return fmt.Errorf("%w: it does not read as the %s it says it is", ErrNotPicture, strings.ToUpper(format))
	}
	if cfg.Width*cfg.Height > maxPixels {
		return fmt.Errorf("%w: %d×%d is over %d megapixels", ErrNotPicture, cfg.Width, cfg.Height, maxPixels/1_000_000)
	}
	return c.write(id.String(), func(w io.Writer) error {
		_, err := w.Write(data)
		return err
	})
}

// Kept answers a picture kept by Keep.
func (c *Cache) Kept(id uuid.UUID) (*os.File, error) {
	return c.root.Open(id.String())
}
