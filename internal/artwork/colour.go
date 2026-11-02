package artwork

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"image"
	"io"
	"math"

	"golang.org/x/image/draw"
)

// Every client takes a picture with no ICC profile to be sRGB. A picture made in another space
// carries a profile saying so, which a copy made here would lose, leaving its colours read as
// sRGB's: a logo in Generic RGB came out dark enough for the web to light it from behind. So a
// picture is brought into sRGB as it is decoded, and its copies, mosaics and BlurHash are of the
// colours it shows. Only a profile of three primaries and their tone curves is followed, as a
// camera's, a display's and an editor's working space are; one of lookup tables, or of grey or
// CMYK, leaves its picture's values as they are.

// maxProfile bounds an embedded profile read; one of primaries and curves is a few KiB.
const maxProfile = 1 << 20

// inSRGB answers img, as the picture format f, read from r, says it is to be seen, in sRGB.
func inSRGB(img image.Image, format string, r io.Reader) image.Image {
	var icc []byte
	br := bufio.NewReader(r)
	switch format {
	case "png":
		icc = pngProfile(br)
	case "jpeg":
		icc = jpegProfile(br)
	case "webp":
		icc = webpProfile(br)
	}
	t, ok := transformOf(icc)
	if !ok {
		return img
	}
	b := img.Bounds()
	dst := image.NewRGBA(b)
	draw.Draw(dst, b, img, b.Min, draw.Src)
	for i := 0; i < len(dst.Pix); i += 4 {
		px := dst.Pix[i : i+4 : i+4]
		a := uint32(px[3])
		if a == 0 {
			continue
		}
		var lin [3]float64
		for k := range 3 {
			v := uint32(px[k])
			if a < 255 {
				v = min(255, (v*255+a/2)/a)
			}
			lin[k] = t.linear[k][v]
		}
		for k := range 3 {
			v := uint32(encodeSRGB(t.matrix[k][0]*lin[0] + t.matrix[k][1]*lin[1] + t.matrix[k][2]*lin[2]))
			if a < 255 {
				v = (v*a + 127) / 255
			}
			px[k] = uint8(v)
		}
	}
	return dst
}

// pngProfile is the profile of a PNG's iCCP chunk, which comes before its pixels.
func pngProfile(r *bufio.Reader) []byte {
	if _, err := r.Discard(8); err != nil {
		return nil
	}
	var head [8]byte
	for {
		if _, err := io.ReadFull(r, head[:]); err != nil {
			return nil
		}
		n := binary.BigEndian.Uint32(head[:4])
		switch string(head[4:]) {
		case "iCCP":
			if n > maxProfile {
				return nil
			}
			chunk := make([]byte, n)
			if _, err := io.ReadFull(r, chunk); err != nil {
				return nil
			}
			// The profile's name, a nul, a compression method that is always zlib's, the profile.
			_, rest, ok := bytes.Cut(chunk, []byte{0})
			if !ok || len(rest) < 1 {
				return nil
			}
			z, err := zlib.NewReader(bytes.NewReader(rest[1:]))
			if err != nil {
				return nil
			}
			icc, err := io.ReadAll(io.LimitReader(z, maxProfile+1))
			if err != nil || len(icc) > maxProfile {
				return nil
			}
			return icc
		case "IDAT", "IEND":
			return nil
		}
		if _, err := r.Discard(int(n) + 4); err != nil {
			return nil
		}
	}
}

// jpegProfile is the profile a JPEG carries in APP2 segments before its scan, in as many parts as
// it takes, each numbered.
func jpegProfile(r *bufio.Reader) []byte {
	if _, err := r.Discard(2); err != nil {
		return nil
	}
	parts := map[byte][]byte{}
	var count byte
	for {
		var m [4]byte
		if _, err := io.ReadFull(r, m[:2]); err != nil || m[0] != 0xff {
			return nil
		}
		for m[1] == 0xff {
			b, err := r.ReadByte()
			if err != nil {
				return nil
			}
			m[1] = b
		}
		if m[1] == 0xda || m[1] == 0xd9 {
			break
		}
		if _, err := io.ReadFull(r, m[2:]); err != nil {
			return nil
		}
		n := int(binary.BigEndian.Uint16(m[2:])) - 2
		if n < 0 {
			return nil
		}
		if m[1] != 0xe2 {
			if _, err := r.Discard(n); err != nil {
				return nil
			}
			continue
		}
		seg := make([]byte, n)
		if _, err := io.ReadFull(r, seg); err != nil {
			return nil
		}
		if rest, ok := bytes.CutPrefix(seg, []byte("ICC_PROFILE\x00")); ok && len(rest) >= 2 {
			parts[rest[0]], count = rest[2:], rest[1]
		}
	}
	var icc []byte
	for i := 1; i <= int(count); i++ {
		part, ok := parts[byte(i)]
		if !ok || len(icc)+len(part) > maxProfile {
			return nil
		}
		icc = append(icc, part...)
	}
	return icc
}

// webpProfile is the profile of a WebP's ICCP chunk, which comes before its pixels.
func webpProfile(r *bufio.Reader) []byte {
	if _, err := r.Discard(12); err != nil {
		return nil
	}
	var head [8]byte
	for {
		if _, err := io.ReadFull(r, head[:]); err != nil {
			return nil
		}
		n := binary.LittleEndian.Uint32(head[4:])
		switch string(head[:4]) {
		case "ICCP":
			if n > maxProfile {
				return nil
			}
			icc := make([]byte, n)
			if _, err := io.ReadFull(r, icc); err != nil {
				return nil
			}
			return icc
		case "VP8 ", "VP8L", "ALPH", "ANIM":
			return nil
		}
		if _, err := r.Discard(int(n + n%2)); err != nil {
			return nil
		}
	}
}

// transform takes a profile's 8-bit values to sRGB: each channel to linear light by its curve,
// then the three by a matrix from its primaries to sRGB's.
type transform struct {
	linear [3][256]float64
	matrix [3][3]float64
}

// srgbFromD50 takes a colour in a profile's connection space, XYZ under D50, to linear sRGB.
var srgbFromD50 = [3][3]float64{
	{3.1338561, -1.6168667, -0.4906146},
	{-0.9787684, 1.9161415, 0.0334540},
	{0.0719453, -0.2289914, 1.4052427},
}

// transformOf reads a profile of primaries and curves, false for any other, and for one that is
// sRGB's own already, as most pictures' are, which has nothing to change.
func transformOf(icc []byte) (transform, bool) {
	var t transform
	if len(icc) < 132 || string(icc[16:20]) != "RGB " || string(icc[20:24]) != "XYZ " {
		return t, false
	}
	tags := map[string][]byte{}
	count := int(binary.BigEndian.Uint32(icc[128:]))
	for i := range min(count, 256) {
		e := 132 + 12*i
		if e+12 > len(icc) {
			return t, false
		}
		at, size := binary.BigEndian.Uint32(icc[e+4:]), binary.BigEndian.Uint32(icc[e+8:])
		if uint64(at)+uint64(size) > uint64(len(icc)) {
			return t, false
		}
		tags[string(icc[e:e+4])] = icc[at : at+size]
	}
	for k, c := range []string{"r", "g", "b"} {
		xyz, ok := xyzOf(tags[c+"XYZ"])
		if !ok {
			return t, false
		}
		curve, ok := curveOf(tags[c+"TRC"])
		if !ok {
			return t, false
		}
		for row := range 3 {
			for i := range 3 {
				t.matrix[row][k] += srgbFromD50[row][i] * xyz[i]
			}
		}
		for v := range 256 {
			t.linear[k][v] = curve(float64(v) / 255)
		}
	}
	return t, !t.isSRGB()
}

// isSRGB is whether t leaves every value within rounding of where it was.
func (t transform) isSRGB() bool {
	for row := range 3 {
		for col := range 3 {
			want := 0.0
			if row == col {
				want = 1
			}
			if math.Abs(t.matrix[row][col]-want) > 0.002 {
				return false
			}
		}
	}
	for k := range 3 {
		for v := range 256 {
			if math.Abs(encodeSRGBExact(t.linear[k][v])*255-float64(v)) > 0.5 {
				return false
			}
		}
	}
	return true
}

func xyzOf(tag []byte) ([3]float64, bool) {
	if len(tag) < 20 || string(tag[:4]) != "XYZ " {
		return [3]float64{}, false
	}
	return [3]float64{s15f16(tag[8:]), s15f16(tag[12:]), s15f16(tag[16:])}, true
}

func s15f16(b []byte) float64 {
	return float64(int32(binary.BigEndian.Uint32(b))) / 65536
}

// curveOf reads a tone curve: a table, a single power, or one of the ICC's parametric families.
func curveOf(tag []byte) (func(float64) float64, bool) {
	if len(tag) < 12 {
		return nil, false
	}
	switch string(tag[:4]) {
	case "curv":
		n := int(binary.BigEndian.Uint32(tag[8:]))
		if len(tag) < 12+2*n {
			return nil, false
		}
		switch n {
		case 0:
			return func(x float64) float64 { return x }, true
		case 1:
			g := float64(binary.BigEndian.Uint16(tag[12:])) / 256
			return func(x float64) float64 { return math.Pow(x, g) }, true
		}
		table := make([]float64, n)
		for i := range n {
			table[i] = float64(binary.BigEndian.Uint16(tag[12+2*i:])) / 65535
		}
		return func(x float64) float64 {
			at := x * float64(n-1)
			i := min(int(at), n-2)
			return table[i] + (table[i+1]-table[i])*(at-float64(i))
		}, true
	case "para":
		kind := binary.BigEndian.Uint16(tag[8:])
		if kind > 4 {
			return nil, false
		}
		want := [5]int{1, 3, 4, 5, 7}[kind]
		if len(tag) < 12+4*want {
			return nil, false
		}
		// g, a, b, c, d, e, f as ICC.1 names them; those a family has not are its identity's.
		p := [7]float64{1, 1, 0, 0, 0, 0, 0}
		for i := range want {
			p[i] = s15f16(tag[12+4*i:])
		}
		g, a, b, c, d, e, f := p[0], p[1], p[2], p[3], p[4], p[5], p[6]
		switch kind {
		case 1:
			d = -b / a
		case 2:
			d, e, f = -b/a, c, c
			c = 0
		}
		return func(x float64) float64 {
			if x >= d {
				return math.Pow(max(a*x+b, 0), g) + e
			}
			return c*x + f
		}, true
	}
	return nil, false
}

// encodeSRGBExact is sRGB's curve, from linear light to a value of 0 to 1.
func encodeSRGBExact(l float64) float64 {
	l = min(max(l, 0), 1)
	if l <= 0.0031308 {
		return 12.92 * l
	}
	return 1.055*math.Pow(l, 1/2.4) - 0.055
}

// srgbSteps is how finely linear light is tabled on its way back to sRGB: enough that the darkest
// steps, where sRGB's curve is steepest, each land on their own value.
const srgbSteps = 1 << 14

var srgbTable = func() (t [srgbSteps + 1]uint8) {
	for i := range t {
		t[i] = uint8(math.Round(encodeSRGBExact(float64(i)/srgbSteps) * 255))
	}
	return t
}()

func encodeSRGB(l float64) uint8 {
	return srgbTable[int(min(max(l, 0), 1)*srgbSteps+0.5)]
}
