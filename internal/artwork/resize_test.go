package artwork

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"testing/synctest"
)

// picture writes a w×h image, opaque or with a transparent corner, and answers how to open it.
func picture(t *testing.T, w, h int, opaque bool) (func(context.Context) (*os.File, error), *int) {
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
	return func(context.Context) (*os.File, error) { opens++; return os.Open(path) }, &opens
}

func TestResized(t *testing.T) {
	c, err := Open(t.TempDir(), nil)
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

// A picture of a few bytes can claim to be 20000×20000; resizing it must not decode that.
func TestAVastPictureIsAnsweredAsItIs(t *testing.T) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewGray(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	b := buf.Bytes()
	// The header chunk's width and height, then its checksum over type and data.
	binary.BigEndian.PutUint32(b[16:], 20000)
	binary.BigEndian.PutUint32(b[20:], 20000)
	binary.BigEndian.PutUint32(b[29:], crc32.ChecksumIEEE(b[12:29]))
	path := filepath.Join(t.TempDir(), "vast.png")
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })

	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	_, err = c.Resized(t.Context(), "vast", 320, func(context.Context) (*os.File, error) { return os.Open(path) })
	runtime.ReadMemStats(&after)
	if !errors.Is(err, ErrNotResizable) {
		t.Errorf("resizing a vast picture: %v, want it answered as it is", err)
	}
	if grew := after.TotalAlloc - before.TotalAlloc; grew > 64<<20 {
		t.Errorf("resizing a %d-byte picture allocated %d MiB", len(b), grew>>20)
	}
}

// Clients asking for one size at once share its making, which carries on when the first goes away.
func TestAResizeOutlivesTheFirstToAsk(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c, err := Open(t.TempDir(), nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = c.Close() })
		poster, _ := picture(t, 1000, 1500, true)
		first, leave := context.WithCancel(t.Context())
		release := make(chan struct{})
		slow := func(ctx context.Context) (*os.File, error) {
			<-release
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			return poster(ctx)
		}
		go func() { _, _ = c.Resized(first, "poster", 320, slow) }()
		stayed := make(chan error)
		go func() {
			f, err := c.Resized(t.Context(), "poster", 320, slow)
			if err == nil {
				_ = f.Close()
			}
			stayed <- err
		}()
		synctest.Wait()
		leave()
		synctest.Wait()
		close(release)
		if err := <-stayed; err != nil {
			t.Errorf("the client still waiting got %v once the first went away, want the picture", err)
		}
	})
}
