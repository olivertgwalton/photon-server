// Package artwork keeps providers' pictures, and titles' theme tunes fetched from ThemerrDB's
// links, once they have been fetched.
package artwork

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"image"
	"io"
	"io/fs"
	"iter"
	"maps"
	"net/http"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"time"
	"uuid"

	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/singleflight"

	"github.com/olivertgwalton/photon-server/internal/blob"
)

const (
	// maxPicture bounds what a provider may send for one picture; posters run to a few MiB.
	maxPicture = 32 << 20
	fetchFor   = 30 * time.Second
)

// objects keeps objects by key, on disk or in a bucket.
type objects interface {
	Open(ctx context.Context, key string) (blob.Object, error)
	Exists(ctx context.Context, key string) (bool, error)
	Put(ctx context.Context, key string, r io.Reader) error
	Delete(ctx context.Context, key string) error
	List(ctx context.Context, prefix string) iter.Seq2[blob.Entry, error]
}

// Cache fetches each picture once, under its id. A picture replaced gets a new id, so an object
// here never goes stale; one replaced is swept away (see Sweep).
type Cache struct {
	blobs objects
	http  *http.Client
	group singleflight.Group
	// resizing holds a place for each picture being resized or hashed, one per processor.
	resizing chan struct{}
	// hashed is told each fetched picture's BlurHash.
	hashed func(ctx context.Context, id uuid.UUID, blurhash string) error
}

func New(blobs objects, hashed func(ctx context.Context, id uuid.UUID, blurhash string) error) *Cache {
	return &Cache{
		blobs: blobs, http: &http.Client{Timeout: fetchFor}, resizing: make(chan struct{}, runtime.NumCPU()),
		hashed: hashed,
	}
}

// File answers the picture with id, fetching it from url the first time it is asked for. Callers
// asking together share one fetch, which carries on if the first of them goes away.
func (c *Cache) File(ctx context.Context, id uuid.UUID, url string) (blob.Object, error) {
	name := id.String()
	if o, err := c.blobs.Open(ctx, name); !errors.Is(err, fs.ErrNotExist) {
		return o, err
	}
	fetched := c.group.DoChan(name, func() (any, error) {
		return nil, c.fetch(context.WithoutCancel(ctx), id, url)
	})
	select {
	case <-ctx.Done():
		return blob.Object{}, ctx.Err()
	case r := <-fetched:
		if r.Err != nil {
			return blob.Object{}, r.Err
		}
	}
	return c.blobs.Open(ctx, name)
}

// fetchTogether is how many pictures Fetch asks for at once; providers limit how fast they are asked.
const fetchTogether = 4

// ErrRefused is a provider refusing to send pictures, or unable to: asking it for more now only
// adds to its load.
var ErrRefused = errors.New("the provider refused the picture")

// Fetch puts pictures, URLs by id, in the cache ahead of their being asked for, and answers how
// many are there now. One that cannot be fetched is passed over, but a provider that refuses one
// stops the run with ErrRefused.
func (c *Cache) Fetch(ctx context.Context, pictures map[uuid.UUID]string) (int, error) {
	var fetched atomic.Int64
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(fetchTogether)
	for id, url := range pictures {
		if gctx.Err() != nil {
			break
		}
		g.Go(func() error {
			// File would start a fetch that outlives its asker even once the run has stopped.
			select {
			case <-gctx.Done():
				return nil
			default:
			}
			f, err := c.File(gctx, id, url)
			if err == nil {
				fetched.Add(1)
				return f.Close()
			}
			return refused(err)
		})
	}
	err := g.Wait()
	return int(fetched.Load()), cmp.Or(err, ctx.Err())
}

// refused is err where it is a provider refusing, so the fetch stops, and nil for any other.
func refused(err error) error {
	if errors.Is(err, ErrRefused) {
		return err
	}
	return nil
}

// fetch keeps the picture at url under id and tells hashed its BlurHash, where it is a picture
// decoded here.
func (c *Cache) fetch(ctx context.Context, id uuid.UUID, url string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= http.StatusInternalServerError {
		return fmt.Errorf("%w: %s %s", ErrRefused, url, resp.Status)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("picture %s: %s", url, resp.Status)
	}
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "image/") {
		return fmt.Errorf("picture %s: %s is not an image", url, resp.Header.Get("Content-Type"))
	}
	body := &atMost{r: resp.Body, left: maxPicture, err: fmt.Errorf("picture %s is over %d bytes", url, maxPicture)}
	if err := c.blobs.Put(ctx, id.String(), body); err != nil {
		return err
	}
	return c.hashAndSize(ctx, id)
}

// hashAndSize decodes the picture kept under id once, for its BlurHash, which hashed is told, and
// the copies a card is drawn at, taking a place among the pictures being resized. An SVG, or a
// picture too vast to decode, is still served; it has no stand-in.
func (c *Cache) hashAndSize(ctx context.Context, id uuid.UUID) error {
	select {
	case c.resizing <- struct{}{}:
		defer func() { <-c.resizing }()
	case <-ctx.Done():
		return ctx.Err()
	}
	o, err := c.blobs.Open(ctx, id.String())
	if err != nil {
		return err
	}
	defer o.Close()
	src, err := decode(o)
	if err != nil {
		return nil
	}
	for _, w := range ahead(src.Bounds()) {
		if err := c.keepResized(ctx, fmt.Sprintf("%s-w%d", id, w), src, w, 0); err != nil && !errors.Is(err, ErrNotResizable) {
			return err
		}
	}
	return c.hashed(ctx, id, blurhashOf(src))
}

// Blurhash answers the BlurHash of the picture kept under id, taking a place among the pictures
// being resized.
func (c *Cache) Blurhash(ctx context.Context, id uuid.UUID) (string, error) {
	select {
	case c.resizing <- struct{}{}:
		defer func() { <-c.resizing }()
	case <-ctx.Done():
		return "", ctx.Err()
	}
	o, err := c.blobs.Open(ctx, id.String())
	if err != nil {
		return "", err
	}
	defer o.Close()
	return Blurhash(o)
}

// atMost reads r until more than left bytes have been read, then fails with err.
type atMost struct {
	r    io.Reader
	left int64
	err  error
}

func (a *atMost) Read(p []byte) (int, error) {
	if a.left < 0 {
		return 0, a.err
	}
	n, err := a.r.Read(p[:min(int64(len(p)), a.left+1)])
	a.left -= int64(n)
	if a.left < 0 {
		return n, a.err
	}
	return n, err
}

// Sweep removes the objects of every picture live says is gone, original and resized copies
// alike. It answers how many it removed.
func (c *Cache) Sweep(ctx context.Context, live func(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]bool, error)) (int, error) {
	byID := map[uuid.UUID][]string{}
	for e, err := range c.blobs.List(ctx, "") {
		if err != nil {
			return 0, err
		}
		name := e.Key
		id, err := uuid.Parse(name[:min(len(name), 36)])
		if err != nil {
			continue
		}
		byID[id] = append(byID[id], name)
	}
	alive, err := live(ctx, slices.Collect(maps.Keys(byID)))
	if err != nil {
		return 0, err
	}
	removed := 0
	for id, names := range byID {
		if alive[id] {
			continue
		}
		for _, name := range names {
			if err := c.blobs.Delete(ctx, name); err != nil {
				return removed, err
			}
			removed++
		}
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
func (c *Cache) Keep(ctx context.Context, id uuid.UUID, r io.Reader) error {
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
	return c.blobs.Put(ctx, id.String(), bytes.NewReader(data))
}

// KeepSound keeps a theme tune the server fetched under id, as it is.
func (c *Cache) KeepSound(ctx context.Context, id uuid.UUID, r io.Reader) error {
	return c.blobs.Put(ctx, id.String(), r)
}

// Kept answers a picture kept by Keep, or a tune kept by KeepSound.
func (c *Cache) Kept(ctx context.Context, id uuid.UUID) (blob.Object, error) {
	return c.blobs.Open(ctx, id.String())
}
