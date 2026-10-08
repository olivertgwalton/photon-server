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
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"testing/synctest"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/blob"
	"github.com/olivertgwalton/photon-server/internal/domain"
)

// picture writes a w×h image, opaque or with a transparent corner, and answers how to open it.
func picture(t *testing.T, w, h int, opaque bool) (func(context.Context) (blob.Object, error), *int) {
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
	return func(context.Context) (blob.Object, error) { opens++; return openFile(path) }, &opens
}

func TestResized(t *testing.T) {
	c := newCache(t, t.TempDir(), nil)

	poster, opens := picture(t, 1000, 1500, true)
	f, err := c.Resized(t.Context(), "poster", 300, 0, poster)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := jpeg.DecodeConfig(f)
	_ = f.Close()
	if err != nil || cfg.Width != 320 || cfg.Height != 480 {
		t.Errorf("300 wide gave %+v, %v; want a 320×480 JPEG, the next width up", cfg, err)
	}
	if _, err := c.Resized(t.Context(), "poster", 320, 0, poster); err != nil || *opens != 1 {
		t.Errorf("asking again read the original %d times, %v; want it made once", *opens, err)
	}
	if _, err := c.Resized(t.Context(), "poster", 1920, 0, poster); !errors.Is(err, ErrNotResizable) {
		t.Errorf("wider than the picture: %v, want it answered as it is", err)
	}
	if _, err := c.Resized(t.Context(), "poster", 1920, 0, poster); !errors.Is(err, ErrNotResizable) || *opens != 2 {
		t.Errorf("asking again for a size it cannot make read the original %d times, want it remembered", *opens)
	}

	logo, _ := picture(t, 800, 300, false)
	f, err = c.Resized(t.Context(), "logo", 400, 0, logo)
	if err != nil {
		t.Fatal(err)
	}
	_, format, err := image.DecodeConfig(f)
	_ = f.Close()
	if err != nil || format != "png" {
		t.Errorf("a transparent logo came back as %q, %v; want PNG, keeping its transparency", format, err)
	}
}

// A width and height bound a box the picture shrinks to fit inside, keeping its shape, each
// rounded up to the next size as a width alone is.
func TestResizedWithinABox(t *testing.T) {
	c := newCache(t, t.TempDir(), nil)
	poster, _ := picture(t, 1000, 1500, true)
	backdrop, _ := picture(t, 1500, 500, true)
	for _, tc := range []struct {
		name          string
		open          func(context.Context) (blob.Object, error)
		width, height int
		wantW, wantH  int
	}{
		{"poster", poster, 0, 400, 320, 480},
		{"poster", poster, 960, 300, 213, 320},
		{"backdrop", backdrop, 640, 640, 640, 213},
		{"poster", poster, 0, 2000, 0, 0},
		{"backdrop", backdrop, 1600, 900, 0, 0},
	} {
		f, err := c.Resized(t.Context(), tc.name, tc.width, tc.height, tc.open)
		if tc.wantW == 0 {
			if !errors.Is(err, ErrNotResizable) {
				t.Errorf("%s within %d×%d: %v, want it as it is, never made larger", tc.name, tc.width, tc.height, err)
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		cfg, _, err := image.DecodeConfig(f)
		_ = f.Close()
		if err != nil || cfg.Width != tc.wantW || cfg.Height != tc.wantH {
			t.Errorf("%s within %d×%d gave %d×%d, %v; want %d×%d", tc.name, tc.width, tc.height, cfg.Width, cfg.Height, err, tc.wantW, tc.wantH)
		}
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
	c := newCache(t, t.TempDir(), nil)

	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	_, err := c.Resized(t.Context(), "vast", 320, 0, func(context.Context) (blob.Object, error) { return openFile(path) })
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
		c := newCache(t, t.TempDir(), nil)
		poster, _ := picture(t, 1000, 1500, true)
		first, leave := context.WithCancel(t.Context())
		release := make(chan struct{})
		slow := func(ctx context.Context) (blob.Object, error) {
			<-release
			if err := ctx.Err(); err != nil {
				return blob.Object{}, err
			}
			return poster(ctx)
		}
		go func() {
			if _, err := c.Resized(first, "poster", 320, 0, slow); !errors.Is(err, context.Canceled) {
				t.Errorf("the client that went away got %v, want %v", err, context.Canceled)
			}
		}()
		stayed := make(chan error)
		go func() {
			f, err := c.Resized(t.Context(), "poster", 320, 0, slow)
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

// A picture is opened from where it is, or as a copy of the size asked for, whose content says its
// format; one that cannot be made smaller is opened as it is, by its own name.
func TestAPictureIsOpenedAsItIsOrToSize(t *testing.T) {
	c := newCache(t, t.TempDir(), nil)
	root := t.TempDir()
	open, _ := picture(t, 1000, 1500, true)
	src, err := open(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(src)
	if err != nil {
		t.Fatal(err)
	}
	_ = src.Close()
	if err := os.WriteFile(filepath.Join(root, "poster.png"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "logo.svg"), []byte("<svg/>"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		path          string
		width, height int
		name          string
	}{
		{"poster.png", 0, 0, "poster.png"},
		{"poster.png", 300, 0, ""},
		{"logo.svg", 300, 0, "logo.svg"},
	} {
		f, name, err := c.Open(t.Context(), uuid.NewV7(), domain.Picture{Root: root, Path: tc.path}, tc.width, tc.height)
		if err != nil {
			t.Fatalf("%s at %d: %v", tc.path, tc.width, err)
		}
		_ = f.Close()
		if name != tc.name {
			t.Errorf("%s at %d: named %q, want %q", tc.path, tc.width, name, tc.name)
		}
	}
}
