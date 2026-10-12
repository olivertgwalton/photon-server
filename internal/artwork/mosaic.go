package artwork

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/jpeg"
	"uuid"

	"golang.org/x/image/draw"

	"github.com/olivertgwalton/photon-server/internal/blob"
	"github.com/olivertgwalton/photon-server/internal/domain"
)

// A mosaic is a poster's shape, two of its tiles across and two down, each a poster at the size a
// card is drawn at its largest.
const (
	tileWidth  = 480
	tileHeight = 720
)

// Mosaic answers the mosaic of tiles kept under id, laying it out the first time it is asked for:
// two by two, in order, and where there are fewer than four, again from the first, crossed where
// there are two, so no tile sits beside itself.
func (c *Cache) Mosaic(ctx context.Context, id uuid.UUID, tiles []domain.Tile) (blob.Object, error) {
	return c.once(ctx, id, func(ctx context.Context) error { return c.layOut(ctx, id, tiles) })
}

func (c *Cache) layOut(ctx context.Context, id uuid.UUID, tiles []domain.Tile) error {
	srcs := make([]image.Image, len(tiles))
	for n, t := range tiles {
		var err error
		if srcs[n], err = c.tile(ctx, t); err != nil {
			return err
		}
	}
	out, err := c.compose(ctx, srcs)
	if err != nil {
		return err
	}
	if err := c.blobs.Put(ctx, id.String(), out); err != nil {
		return err
	}
	return c.hashAndSize(ctx, id)
}

// tile reads t from its copy at a tile's width, which a poster is kept at as it is fetched, rather
// than its original: decoding four originals of a provider's size took twelve times as long. It is
// opened, and fetched where it has not been, before a place is taken, as resize does.
func (c *Cache) tile(ctx context.Context, t domain.Tile) (image.Image, error) {
	original := func(ctx context.Context) (blob.Object, error) { return c.original(ctx, t.ID, t.Picture) }
	f, err := c.Resized(ctx, t.ID.String(), tileWidth, 0, original)
	if errors.Is(err, ErrNotResizable) {
		f, err = original(ctx)
	}
	if err != nil {
		return nil, err
	}
	src, err := decode(f)
	return src, errors.Join(err, f.Close())
}

// compose lays srcs out two by two as a JPEG, taking a place among the pictures being resized.
func (c *Cache) compose(ctx context.Context, srcs []image.Image) (*bytes.Buffer, error) {
	select {
	case c.resizing <- struct{}{}:
		defer func() { <-c.resizing }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	dst := image.NewRGBA(image.Rect(0, 0, 2*tileWidth, 2*tileHeight))
	for n, pick := range layout(len(srcs)) {
		at := image.Rect(0, 0, tileWidth, tileHeight).Add(image.Pt(n%2*tileWidth, n/2*tileHeight))
		draw.CatmullRom.Scale(dst, at, srcs[pick], cover(srcs[pick].Bounds(), tileWidth, tileHeight), draw.Src, nil)
	}
	var out bytes.Buffer
	return &out, jpeg.Encode(&out, dst, &jpeg.Options{Quality: 85})
}

// layout is which of n pictures each of a mosaic's four tiles shows, left to right and down.
func layout(n int) [4]int {
	switch n {
	case 2:
		return [4]int{0, 1, 1, 0}
	case 3:
		return [4]int{0, 1, 2, 0}
	}
	return [4]int{0, 1, 2, 3}
}

// cover is the middle of b in the shape of a width×height tile, so a picture of another shape is
// cut to fill it rather than squashed.
func cover(b image.Rectangle, width, height int) image.Rectangle {
	w, h := min(b.Dx(), b.Dy()*width/height), min(b.Dy(), b.Dx()*height/width)
	return image.Rect(0, 0, w, h).Add(b.Min).Add(image.Pt((b.Dx()-w)/2, (b.Dy()-h)/2))
}
