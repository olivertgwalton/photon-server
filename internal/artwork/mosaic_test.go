package artwork

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"testing"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// Two posters make a poster-shaped mosaic crossed two by two, kept and hashed as a fetched picture
// is, so it is laid out once.
func TestTwoPostersAreLaidOutCrossed(t *testing.T) {
	var hashedID uuid.UUID
	c := newCache(t, t.TempDir(), func(_ context.Context, id uuid.UUID, hash string) error {
		if hash == "" {
			t.Error("a mosaic hashed to nothing")
		}
		hashedID = id
		return nil
	})
	ctx := t.Context()
	solid := func(c color.Color, w, h int) []byte {
		img := image.NewRGBA(image.Rect(0, 0, w, h))
		for y := range h {
			for x := range w {
				img.Set(x, y, c)
			}
		}
		var b bytes.Buffer
		if err := png.Encode(&b, img); err != nil {
			t.Fatal(err)
		}
		return b.Bytes()
	}
	red, blue := uuid.NewV7(), uuid.NewV7()
	// The blue one is wider than a poster, so it is cut to fill its tile.
	for id, pic := range map[uuid.UUID][]byte{red: solid(color.RGBA{255, 0, 0, 255}, 40, 60), blue: solid(color.RGBA{0, 0, 255, 255}, 90, 60)} {
		if err := c.Keep(ctx, id, bytes.NewReader(pic)); err != nil {
			t.Fatal(err)
		}
	}
	mosaic := uuid.NewV7()
	tiles := []domain.Tile{{ID: red, Picture: domain.Picture{Kept: true}}, {ID: blue, Picture: domain.Picture{Kept: true}}}
	o, err := c.Mosaic(ctx, mosaic, tiles)
	if err != nil {
		t.Fatal(err)
	}
	defer o.Close()
	img, format, err := image.Decode(o)
	if err != nil || format != "jpeg" {
		t.Fatalf("mosaic decodes as %q, %v", format, err)
	}
	if b := img.Bounds(); b.Dx()*3 != b.Dy()*2 {
		t.Errorf("mosaic is %v, want a poster's shape", b)
	}
	at := func(x, y float64) string {
		r, _, bl, _ := img.At(int(x*float64(img.Bounds().Dx())), int(y*float64(img.Bounds().Dy()))).RGBA()
		switch {
		case r > 0xc000 && bl < 0x4000:
			return "red"
		case bl > 0xc000 && r < 0x4000:
			return "blue"
		}
		return "other"
	}
	if got := []string{at(0.25, 0.25), at(0.75, 0.25), at(0.25, 0.75), at(0.75, 0.75)}; got[0] != "red" || got[1] != "blue" || got[2] != "blue" || got[3] != "red" {
		t.Errorf("tiles = %v, want red and blue crossed", got)
	}
	if hashedID != mosaic {
		t.Errorf("hashed %v, want the mosaic %v", hashedID, mosaic)
	}
	// Asked again it is the one kept, not laid out anew.
	again, _, err := c.Open(ctx, mosaic, domain.Picture{Mosaic: tiles}, 320, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := again.Close(); err != nil {
		t.Fatal(err)
	}
}
