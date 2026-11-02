package artwork

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/olivertgwalton/photon-server/internal/blob"
)

type iccTag struct {
	sig  string
	data []byte
}

// iccProfile is a profile of sRGB's primaries whose three channels share one tone curve, and any
// other tags.
func iccProfile(curve []byte, others ...iccTag) []byte {
	xyz := func(x, y, z float64) []byte {
		b := []byte("XYZ \x00\x00\x00\x00")
		for _, v := range []float64{x, y, z} {
			b = binary.BigEndian.AppendUint32(b, uint32(int32(v*65536)))
		}
		return b
	}
	tags := append([]iccTag{
		{"rXYZ", xyz(0.4360747, 0.2225045, 0.0139322)},
		{"gXYZ", xyz(0.3850649, 0.7168786, 0.0971045)},
		{"bXYZ", xyz(0.1430804, 0.0606169, 0.7141733)},
		{"rTRC", curve},
		{"gTRC", curve},
		{"bTRC", curve},
	}, others...)
	head := make([]byte, 128)
	copy(head[16:], "RGB XYZ ")
	table := binary.BigEndian.AppendUint32(nil, uint32(len(tags)))
	var data []byte
	at := 128 + 4 + 12*len(tags)
	for _, t := range tags {
		table = append(table, t.sig...)
		table = binary.BigEndian.AppendUint32(table, uint32(at+len(data)))
		table = binary.BigEndian.AppendUint32(table, uint32(len(t.data)))
		data = append(data, t.data...)
	}
	icc := append(append(head, table...), data...)
	binary.BigEndian.PutUint32(icc, uint32(len(icc)))
	return icc
}

// gamma18 is a plain power of 1.8, as Apple's Generic RGB has.
var gamma18 = []byte("curv\x00\x00\x00\x00\x00\x00\x00\x01\x01\xcc")

// paraCurve is a curve of ICC's parametric family kind, of params.
func paraCurve(kind byte, params ...float64) []byte {
	b := []byte("para\x00\x00\x00\x00\x00\x00\x00\x00")
	b[9] = kind
	for _, v := range params {
		b = binary.BigEndian.AppendUint32(b, uint32(int32(math.Round(v*65536))))
	}
	return b
}

// srgbCurve is sRGB's own curve, as ICC's fourth parametric family.
func srgbCurve() []byte {
	return paraCurve(3, 2.4, 1/1.055, 0.055/1.055, 1/12.92, 0.04045)
}

// taggedPNG is a PNG of img carrying icc in an iCCP chunk, or none where icc is nil.
func taggedPNG(tb testing.TB, img image.Image, icc []byte) []byte {
	tb.Helper()
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		tb.Fatal(err)
	}
	if icc == nil {
		return out.Bytes()
	}
	var z bytes.Buffer
	w := zlib.NewWriter(&z)
	if _, err := w.Write(icc); err != nil {
		tb.Fatal(err)
	}
	if err := w.Close(); err != nil {
		tb.Fatal(err)
	}
	body := append([]byte("iCCP"), append([]byte("test\x00\x00"), z.Bytes()...)...)
	chunk := binary.BigEndian.AppendUint32(nil, uint32(len(body)-4))
	chunk = append(chunk, body...)
	chunk = binary.BigEndian.AppendUint32(chunk, crc32.ChecksumIEEE(body))
	b := out.Bytes()
	// After the signature and IHDR, before the pixels.
	return append(append(append([]byte{}, b[:33]...), chunk...), b[33:]...)
}

// taggedJPEG is a JPEG of img carrying icc in an APP2 segment.
func taggedJPEG(tb testing.TB, img image.Image, icc []byte) []byte {
	tb.Helper()
	var out bytes.Buffer
	if err := jpeg.Encode(&out, img, &jpeg.Options{Quality: 100}); err != nil {
		tb.Fatal(err)
	}
	body := append([]byte("ICC_PROFILE\x00\x01\x01"), icc...)
	seg := append([]byte{0xff, 0xe2}, binary.BigEndian.AppendUint16(nil, uint16(len(body)+2))...)
	b := out.Bytes()
	return append(append(append([]byte{}, b[:2]...), append(seg, body...)...), b[2:]...)
}

func uniform(c color.NRGBA) image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, 640, 320))
	for i := 0; i < len(img.Pix); i += 4 {
		copy(img.Pix[i:], []byte{c.R, c.G, c.B, c.A})
	}
	return img
}

// copyOf answers the middle of the 320-wide copy made of the picture data holds.
func copyOf(t *testing.T, data []byte) color.NRGBA {
	t.Helper()
	path := filepath.Join(t.TempDir(), "picture")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	c := newCache(t, t.TempDir(), nil)
	f, err := c.Resized(t.Context(), "picture", 320, 0, func(context.Context) (blob.Object, error) { return openFile(path) })
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, _, err := image.Decode(io.Reader(f))
	if err != nil {
		t.Fatal(err)
	}
	return color.NRGBAModel.Convert(img.At(160, 80)).(color.NRGBA)
}

func near(got, want color.NRGBA, by int) bool {
	d := func(a, b uint8) bool { return int(a)-int(b) <= by && int(b)-int(a) <= by }
	return d(got.R, want.R) && d(got.G, want.G) && d(got.B, want.B) && d(got.A, want.A)
}

// A copy shows a picture's colours as its profile says they are, so a client reading the copy as
// sRGB, as it reads any untagged picture, sees what it would have seen in the original.
func TestACopyKeepsThePictureColoursItsProfileGives(t *testing.T) {
	grey := color.NRGBA{R: 128, G: 128, B: 128, A: 255}
	// Mid grey under a power of 1.8 is sRGB's 146.
	lighter := color.NRGBA{R: 146, G: 146, B: 146, A: 255}
	for _, c := range []struct {
		name string
		data []byte
		want color.NRGBA
		by   int
	}{
		{"untagged", taggedPNG(t, uniform(grey), nil), grey, 0},
		{"tagged sRGB", taggedPNG(t, uniform(grey), iccProfile(srgbCurve())), grey, 0},
		{"a PNG in gamma 1.8", taggedPNG(t, uniform(grey), iccProfile(gamma18)), lighter, 1},
		{"a JPEG in gamma 1.8", taggedJPEG(t, uniform(grey), iccProfile(gamma18)), lighter, 2},
		{
			// Every browser draws such a profile by its tables, which are not read here.
			"a PNG with lookup tables as well",
			taggedPNG(t, uniform(grey), iccProfile(gamma18, iccTag{"A2B0", []byte("mft2\x00\x00\x00\x00")})),
			grey,
			0,
		},
		{"a PNG whose curve is infinite at black", taggedPNG(t, uniform(grey), iccProfile(paraCurve(0, -1))), grey, 0},
		{
			"a half-clear logo in gamma 1.8",
			taggedPNG(t, uniform(color.NRGBA{R: 128, G: 128, B: 128, A: 128}), iccProfile(gamma18)),
			color.NRGBA{R: 146, G: 146, B: 146, A: 128},
			2,
		},
	} {
		if got := copyOf(t, c.data); !near(got, c.want, c.by) {
			t.Errorf("%s: copy is %v, want %v", c.name, got, c.want)
		}
	}
}

// No picture, whatever its profile says, stops the server reading it.
func FuzzDecode(f *testing.F) {
	small := uniform(color.NRGBA{R: 128, G: 64, B: 32, A: 200})
	f.Add(taggedPNG(f, small, iccProfile(gamma18)))
	f.Add(taggedPNG(f, small, iccProfile(paraCurve(4, 2.4, 1, 0, 1, 0.5, 0, 0))))
	f.Add(taggedJPEG(f, small, iccProfile(srgbCurve())))
	f.Fuzz(func(t *testing.T, data []byte) {
		if img, err := decode(bytes.NewReader(data)); err == nil && img == nil {
			t.Error("decoded to nothing, with no error")
		}
	})
}
