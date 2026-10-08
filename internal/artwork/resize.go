package artwork

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"io/fs"
	"path"
	"slices"
	"uuid"

	"golang.org/x/image/draw"

	"github.com/olivertgwalton/photon-server/internal/blob"
	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/library"

	// Decoders for the pictures providers and libraries hold.
	_ "image/gif"

	_ "golang.org/x/image/webp"
)

// sizes are the bounds a picture is made within: a width or height asked for is rounded up to the
// next, so each picture is kept at a handful of sizes whatever clients ask for.
var sizes = []int{160, 320, 480, 640, 960, 1280, 1920, 2560, 3840}

// The widths a provider's picture is kept at as it is fetched, before anything asks, so a wall
// of titles just added draws without a decode a card: a portrait picture's are a poster's, a
// landscape one's a still's. Anything wider, as a title page's backdrop is, is made as asked for.
var (
	portraitAhead  = []int{160, 320, 480}
	landscapeAhead = []int{320, 480, 640, 960}
)

// maxPixels bounds the pictures decoded here: a few MiB of PNG can claim a picture that takes GiBs
// to decode. A 4K backdrop is 8 megapixels.
const maxPixels = 50_000_000

// ErrNotResizable is a picture answered as it is: one that fits what was asked for already, or not
// decoded here, such as SVG.
var ErrNotResizable = errors.New("picture cannot be resized")

// Open opens picture id, or with width or height a copy that fits inside them where it can be
// made, and answers the name its format is known by: "" for a copy, whose content says.
func (c *Cache) Open(ctx context.Context, id uuid.UUID, p domain.Picture, width, height int) (blob.Object, string, error) {
	name := path.Base(p.Path + p.URL)
	original := func(ctx context.Context) (blob.Object, error) {
		switch {
		case p.Kept:
			return c.Kept(ctx, id)
		case p.URL != "":
			return c.File(ctx, id, p.URL)
		}
		f, err := library.Open(p.Root, p.Path)
		if err != nil {
			return blob.Object{}, err
		}
		return blob.OfFile(f)
	}
	if width > 0 || height > 0 {
		f, err := c.Resized(ctx, id.String(), width, height, original)
		if !errors.Is(err, ErrNotResizable) {
			return f, "", err
		}
	}
	f, err := original(ctx)
	return f, name, err
}

// Resized answers the picture with key shrunk to fit inside width×height, keeping its shape, where
// a bound of 0 is none; or, where it fits already, as it is (ErrNotResizable). open reads the
// picture's own file, on a context that outlives the callers, as everyone asking for the size at
// once shares it. Each size is made once and kept.
func (c *Cache) Resized(ctx context.Context, key string, width, height int, open func(context.Context) (blob.Object, error)) (blob.Object, error) {
	name := key
	if width > 0 {
		width = roundUp(width)
		name += fmt.Sprintf("-w%d", width)
	}
	if height > 0 {
		height = roundUp(height)
		name += fmt.Sprintf("-h%d", height)
	}
	if o, err := c.blobs.Open(ctx, name); !errors.Is(err, fs.ErrNotExist) {
		return o, err
	}
	if asIs, err := c.blobs.Exists(ctx, name+".as-is"); asIs || err != nil {
		return blob.Object{}, cmp.Or(err, ErrNotResizable)
	}
	made := c.group.DoChan(name, func() (any, error) {
		return nil, c.resize(context.WithoutCancel(ctx), name, width, height, open)
	})
	select {
	case <-ctx.Done():
		return blob.Object{}, ctx.Err()
	case r := <-made:
		if r.Err != nil {
			return blob.Object{}, r.Err
		}
	}
	return c.blobs.Open(ctx, name)
}

func roundUp(n int) int {
	i, _ := slices.BinarySearch(sizes, n)
	return sizes[min(i, len(sizes)-1)]
}

func (c *Cache) resize(ctx context.Context, name string, width, height int, open func(context.Context) (blob.Object, error)) error {
	// Opened before taking a place: a provider's picture is fetched and hashed on first open, and
	// hashing takes a place of its own, so resizes holding every place would wait on it for good.
	f, err := open(ctx)
	if err != nil {
		return err
	}
	select {
	case c.resizing <- struct{}{}:
		defer func() { <-c.resizing }()
	case <-ctx.Done():
		return errors.Join(ctx.Err(), f.Close())
	}
	src, err := decode(f)
	if cerr := f.Close(); cerr != nil {
		return cerr
	}
	if err != nil {
		// A picture that cannot be decoded (a format not decoded here, or a damaged or vast file)
		// is marked, so the next ask does not try again.
		return c.asIs(ctx, name)
	}
	return c.keepResized(ctx, name, src, width, height)
}

// keepResized keeps src shrunk to fit inside width×height under name; one that fits already is
// marked answered as it is (ErrNotResizable), so the next ask does not decode it again.
func (c *Cache) keepResized(ctx context.Context, name string, src image.Image, width, height int) error {
	b := src.Bounds()
	w, h := fit(b.Dx(), b.Dy(), width, height)
	if w == b.Dx() && h == b.Dy() {
		return c.asIs(ctx, name)
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Src, nil)
	var out bytes.Buffer
	var err error
	if dst.Opaque() {
		err = jpeg.Encode(&out, dst, &jpeg.Options{Quality: 85})
	} else {
		err = png.Encode(&out, dst)
	}
	if err != nil {
		return err
	}
	return c.blobs.Put(ctx, name, &out)
}

// asIs marks the copy name as one the picture is answered as it is for.
func (c *Cache) asIs(ctx context.Context, name string) error {
	if err := c.blobs.Put(ctx, name+".as-is", bytes.NewReader(nil)); err != nil {
		return err
	}
	return ErrNotResizable
}

// ahead are the widths a fetched picture of bounds b is kept at before anything asks.
func ahead(b image.Rectangle) []int {
	if b.Dx() > b.Dy() {
		return landscapeAhead
	}
	return portraitAhead
}

// fit answers the size a dx×dy picture shrinks to inside width×height, where a bound of 0 is none.
func fit(dx, dy, width, height int) (w, h int) {
	w, h = dx, dy
	if width > 0 && w > width {
		w, h = width, dy*width/dx
	}
	if height > 0 && h > height {
		w, h = dx*height/dy, height
	}
	return max(w, 1), max(h, 1)
}

// decode reads a picture of at most maxPixels.
func decode(f io.ReadSeeker) (image.Image, error) {
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return nil, err
	}
	if cfg.Width*cfg.Height > maxPixels {
		return nil, fmt.Errorf("picture is %d×%d, over %d pixels", cfg.Width, cfg.Height, maxPixels)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	src, _, err := image.Decode(f)
	return src, err
}
