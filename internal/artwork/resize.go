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

// sizes are the bounds a picture is made within: a width or height asked for is rounded up to the
// next, so each picture is kept at a handful of sizes whatever clients ask for.
var sizes = []int{160, 320, 480, 640, 960, 1280, 1920, 2560, 3840}

// maxPixels bounds the pictures decoded here: a few MiB of PNG can claim a picture that takes GiBs
// to decode. A 4K backdrop is 8 megapixels.
const maxPixels = 50_000_000

// ErrNotResizable is a picture answered as it is: one that fits what was asked for already, or not
// decoded here, such as SVG.
var ErrNotResizable = errors.New("picture cannot be resized")

// Resized answers the picture with key shrunk to fit inside width×height, keeping its shape, where
// a bound of 0 is none; or, where it fits already, as it is (ErrNotResizable). open reads the
// picture's own file, on a context that outlives the callers, as everyone asking for the size at
// once shares it. Each size is made once and kept.
func (c *Cache) Resized(ctx context.Context, key string, width, height int, open func(context.Context) (*os.File, error)) (*os.File, error) {
	name := key
	if width > 0 {
		width = roundUp(width)
		name += fmt.Sprintf("-w%d", width)
	}
	if height > 0 {
		height = roundUp(height)
		name += fmt.Sprintf("-h%d", height)
	}
	if f, err := c.root.Open(name); !errors.Is(err, fs.ErrNotExist) {
		return f, err
	}
	if _, err := c.root.Stat(name + ".as-is"); err == nil {
		return nil, ErrNotResizable
	}
	made := c.group.DoChan(name, func() (any, error) {
		return nil, c.resize(context.WithoutCancel(ctx), name, width, height, open)
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

func roundUp(n int) int {
	i, _ := slices.BinarySearch(sizes, n)
	return sizes[min(i, len(sizes)-1)]
}

func (c *Cache) resize(ctx context.Context, name string, width, height int, open func(context.Context) (*os.File, error)) error {
	select {
	case c.resizing <- struct{}{}:
		defer func() { <-c.resizing }()
	case <-ctx.Done():
		return ctx.Err()
	}
	f, err := open(ctx)
	if err != nil {
		return err
	}
	src, err := decode(f)
	_ = f.Close()
	var b image.Rectangle
	var w, h int
	if err == nil {
		b = src.Bounds()
		w, h = fit(b.Dx(), b.Dy(), width, height)
	}
	// A picture that cannot be made smaller (a format not decoded here, a damaged or vast file, or
	// one that fits already) is marked, so the next ask does not decode it again.
	if err != nil || w == b.Dx() && h == b.Dy() {
		if err := c.write(name+".as-is", func(io.Writer) error { return nil }); err != nil {
			return err
		}
		return ErrNotResizable
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Src, nil)
	return c.write(name, func(w io.Writer) error {
		if dst.Opaque() {
			return jpeg.Encode(w, dst, &jpeg.Options{Quality: 85})
		}
		return png.Encode(w, dst)
	})
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
