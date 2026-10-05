package artwork

import (
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// picture writes a w×h image, opaque or with a transparent corner, and answers how to open it.
func picture(t *testing.T, w, h int, opaque bool) (func() (*os.File, error), *int) {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for x := range w {
		for y := range h {
			img.Set(x, y, color.NRGBA{R: uint8(x), G: uint8(y), B: 90, A: 255})
		}
	}
	if !opaque {
		img.Set(0, 0, color.NRGBA{})
	}
	path := filepath.Join(t.TempDir(), "picture.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	opens := 0
	return func() (*os.File, error) { opens++; return os.Open(path) }, &opens
}

func TestResized(t *testing.T) {
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })

	poster, opens := picture(t, 1000, 1500, true)
	f, err := c.Resized(t.Context(), "poster", 300, poster)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := jpeg.DecodeConfig(f)
	_ = f.Close()
	if err != nil || cfg.Width != 320 || cfg.Height != 480 {
		t.Errorf("300 wide gave %+v, %v; want a 320×480 JPEG, the next width up", cfg, err)
	}
	if _, err := c.Resized(t.Context(), "poster", 320, poster); err != nil || *opens != 1 {
		t.Errorf("asking again read the original %d times, %v; want it made once", *opens, err)
	}
	if _, err := c.Resized(t.Context(), "poster", 1920, poster); !errors.Is(err, ErrNotResizable) {
		t.Errorf("wider than the picture: %v, want it answered as it is", err)
	}
	if _, err := c.Resized(t.Context(), "poster", 1920, poster); !errors.Is(err, ErrNotResizable) || *opens != 2 {
		t.Errorf("asking again for a size it cannot make read the original %d times, want it remembered", *opens)
	}

	logo, _ := picture(t, 800, 300, false)
	f, err = c.Resized(t.Context(), "logo", 400, logo)
	if err != nil {
		t.Fatal(err)
	}
	_, format, err := image.DecodeConfig(f)
	_ = f.Close()
	if err != nil || format != "png" {
		t.Errorf("a transparent logo came back as %q, %v; want PNG, keeping its transparency", format, err)
	}
}
