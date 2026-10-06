package artwork

import (
	"bytes"
	"image"
	"image/png"
	"strings"
	"testing"
)

// gradient is the picture the reference encoder was run over for the hashes below.
func gradient(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			p := img.Pix[y*img.Stride+x*4:]
			p[0], p[1], p[2], p[3] = uint8(x*255/(w-1)), uint8(y*255/(h-1)), uint8((x+y)*37%256), 255
		}
	}
	return img
}

// The hashes are what woltapp/blurhash's C encoder answers for the same pixels.
func TestBlurhashMatchesTheReferenceEncoder(t *testing.T) {
	for _, c := range []struct {
		w, h, cx, cy int
		want         string
	}{
		{32, 48, 4, 5, "d$HetU2pwxX8l^agjse;gcfjfQfjnSa|jsfQfjfQfQfQ"},
		{64, 36, 6, 4, "W$HVC,2V$4Sho0bIl{WVjuf6fRf6gIfkfPfkfPfjnmWojufQfQfQ"},
		{20, 20, 1, 1, "00HoH{"},
		{40, 30, 9, 9, "|$HetS2o$4X8jskCWoofWol{WVjtf7fRf7fRf6fRgIfjfPfjfPfjfPfifPnmWojufQfRfQfRfQfRf6fQfPfPfPfPfPfPfPofWojufQfRfQfRfQfRe:fPfPfPfPfPfPfPfPofWojufPfRfPfRfPfRe:fPfPfPfPfOfPfOfP"},
	} {
		if got := encodeBlurhash(gradient(c.w, c.h), c.cx, c.cy); got != c.want {
			t.Errorf("%dx%d at %dx%d components: %s, want %s", c.w, c.h, c.cx, c.cy, got, c.want)
		}
	}
}

func TestBlurhashFollowsThePictureShape(t *testing.T) {
	for _, c := range []struct {
		w, h int
		size byte // the hash's first digit: its components across, less one, plus nine times those down, less one
	}{
		{2000, 3000, digits83[3+4*9]}, // a poster: 4 across, 5 down
		{3840, 2160, digits83[5+3*9]}, // a backdrop: 6 across, 4 down
	} {
		var buf bytes.Buffer
		if err := png.Encode(&buf, gradient(c.w/40, c.h/40)); err != nil {
			t.Fatal(err)
		}
		got, err := Blurhash(bytes.NewReader(buf.Bytes()))
		if err != nil {
			t.Fatal(err)
		}
		if got[0] != c.size {
			t.Errorf("%dx%d: %s starts %c, want %c", c.w, c.h, got, got[0], c.size)
		}
	}
	if _, err := Blurhash(strings.NewReader("<svg/>")); err == nil {
		t.Error("an SVG was hashed")
	}
}
