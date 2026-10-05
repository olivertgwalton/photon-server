package artwork

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"io/fs"
	"os"
	"slices"

	"golang.org/x/image/draw"

	// Decoders for the pictures providers and libraries hold.
	_ "image/gif"

	_ "golang.org/x/image/webp"
)

// Widths are the sizes a picture is made at: a width asked for is rounded up to the next, so each
// picture is kept at a handful of sizes whatever clients ask for.
var Widths = []int{160, 320, 480, 640, 960, 1280, 1920, 2560, 3840}

// maxPixels bounds the pictures decoded here: a few MiB of PNG can claim a picture that takes GiBs
// to decode. A 4K backdrop is 8 megapixels.
const maxPixels = 50_000_000

// ErrNotResizable is a picture answered as it is: one no wider than asked for, or not decoded
// here, such as SVG.
var ErrNotResizable = errors.New("picture cannot be resized")

// Resized answers the picture with key at width or, where it is no wider than that, as it is
// (ErrNotResizable). open reads the picture's own file. Each size is made once and kept.
func (c *Cache) Resized(ctx context.Context, key string, width int, open func() (*os.File, error)) (*os.File, error) {
	i, _ := slices.BinarySearch(Widths, width)
	width = Widths[min(i, len(Widths)-1)]
	name := fmt.Sprintf("%s-w%d", key, width)
	if f, err := c.root.Open(name); !errors.Is(err, fs.ErrNotExist) {
		return f, err
	}
	if _, err := c.root.Stat(name + ".as-is"); err == nil {
		return nil, ErrNotResizable
	}
	made := c.group.DoChan(name, func() (any, error) {
		return nil, c.resize(context.WithoutCancel(ctx), name, width, open)
	})
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case r := <-made:
		if r.Err != nil {
			return nil, r.Err
		}
	}
	return c.root.Open(name)
}

func (c *Cache) resize(ctx context.Context, name string, width int, open func() (*os.File, error)) error {
	select {
	case c.resizing <- struct{}{}:
		defer func() { <-c.resizing }()
	case <-ctx.Done():
		return ctx.Err()
	}
	f, err := open()
	if err != nil {
		return err
	}
	src, err := decode(f)
	_ = f.Close()
	// A picture that cannot be made smaller (a format not decoded here, a damaged or vast file, or
	// one already no wider) is marked, so the next ask does not decode it again.
	if err != nil || src.Bounds().Dx() <= width {
		if err := c.write(name+".as-is", func(io.Writer) error { return nil }); err != nil {
			return err
		}
		return ErrNotResizable
	}
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, width, b.Dy()*width/b.Dx()))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Src, nil)
	return c.write(name, func(w io.Writer) error {
		if dst.Opaque() {
			return jpeg.Encode(w, dst, &jpeg.Options{Quality: 85})
		}
		return png.Encode(w, dst)
	})
}

// decode reads a picture of at most maxPixels.
func decode(f *os.File) (image.Image, error) {
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

// write makes file name whole before it can be read.
func (c *Cache) write(name string, encode func(io.Writer) error) error {
	part := name + ".part"
	f, err := c.root.Create(part)
	if err != nil {
		return err
	}
	err = encode(f)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = c.root.Remove(part)
		return err
	}
	return c.root.Rename(part, name)
}
