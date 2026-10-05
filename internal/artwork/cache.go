// Package artwork keeps providers' pictures on disk once they have been fetched.
package artwork

import (
	"context"
	"errors"
	"fmt"
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
	// maxPicture bounds what a provider may send for one picture; posters run to a few MiB.
	maxPicture = 32 << 20
	fetchFor   = 30 * time.Second
)

// Cache fetches each picture once, under its id. A picture replaced gets a new id, so a file
// here never goes stale.
// ponytail: a replaced picture's file stays until the cache folder is cleared; a sweep against
// the artwork table when disk use matters.
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
	name := id.String()
	if f, err := c.root.Open(name); !errors.Is(err, fs.ErrNotExist) {
		return f, err
	}
	fetched := c.group.DoChan(name, func() (any, error) {
		return nil, c.fetch(context.WithoutCancel(ctx), name, url)
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

func (c *Cache) fetch(ctx context.Context, name, url string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("picture %s: %s", url, resp.Status)
	}
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "image/") {
		return fmt.Errorf("picture %s: %s is not an image", url, resp.Header.Get("Content-Type"))
	}
	return c.write(name, func(w io.Writer) error {
		n, err := io.Copy(w, io.LimitReader(resp.Body, maxPicture+1))
		if err == nil && n > maxPicture {
			err = fmt.Errorf("picture %s is over %d bytes", url, maxPicture)
		}
		return err
	})
}
