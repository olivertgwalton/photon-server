package artwork

import (
	"image"
	"io"
	"math"

	"golang.org/x/image/draw"

	"github.com/olivertgwalton/photon-server/internal/library"
)

// hashedEdge is the longest side a picture is shrunk to before its BlurHash is taken: a blur of
// a few components looks the same from any more, and every pixel costs a cosine per component.
const hashedEdge = 64

// Blurhash answers a BlurHash of the picture r holds (https://blurha.sh), for a client to draw
// blurred before the picture arrives. Its components follow the picture's shape as Jellyfin's
// do: tiles as near square as fit about sixteen of them.
func Blurhash(r io.ReadSeeker) (string, error) {
	src, err := decode(r)
	if err != nil {
		return "", err
	}
	return blurhashOf(src), nil
}

// blurhashOf is the BlurHash of a decoded picture.
func blurhashOf(src image.Image) string {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w > h {
		w, h = hashedEdge, max(1, h*hashedEdge/w)
	} else {
		w, h = max(1, w*hashedEdge/h), hashedEdge
	}
	small := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.ApproxBiLinear.Scale(small, small.Bounds(), src, b, draw.Src, nil)
	x := math.Sqrt(16 * float64(b.Dx()) / float64(b.Dy()))
	y := x * float64(b.Dy()) / float64(b.Dx())
	return encodeBlurhash(small, min(int(x)+1, 9), min(int(y)+1, 9))
}

// FileBlurhash answers the BlurHash of the library file at rel under root.
func FileBlurhash(root, rel string) (string, error) {
	f, err := library.Open(root, rel)
	if err != nil {
		return "", err
	}
	defer f.Close()
	return Blurhash(f)
}

// encodeBlurhash is the reference encoder (woltapp/blurhash's C), over an RGBA picture. A BlurHash
// has no alpha, and the reference reads a transparent pixel as the black image.RGBA stores it as:
// a logo's hash came out its letters blurred on black, its colour near black whatever the letters'.
// Here each pixel counts as much as it is opaque, so a logo's hash is the colour of its letters, and
// an opaque picture's is the reference's.
func encodeBlurhash(img *image.RGBA, cx, cy int) string {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	linear := make([][3]float64, w*h)
	weight := make([]float64, w*h)
	var opaque float64
	for y := range h {
		for x := range w {
			p := img.Pix[y*img.Stride+x*4:]
			if p[3] == 0 {
				continue
			}
			// image.RGBA holds colour multiplied by alpha.
			c := func(v uint8) float64 { return toLinear(uint8(min(255, int(v)*255/int(p[3])))) }
			linear[y*w+x] = [3]float64{c(p[0]), c(p[1]), c(p[2])}
			weight[y*w+x] = float64(p[3]) / 255
			opaque += weight[y*w+x]
		}
	}
	if opaque == 0 {
		opaque = float64(w * h)
	}
	factors := make([][3]float64, 0, cx*cy)
	for j := range cy {
		for i := range cx {
			norm := 2.0
			if i == 0 && j == 0 {
				norm = 1
			}
			var f [3]float64
			for y := range h {
				cosY := math.Cos(math.Pi * float64(j) * float64(y) / float64(h))
				for x := range w {
					basis := norm * math.Cos(math.Pi*float64(i)*float64(x)/float64(w)) * cosY
					for c := range 3 {
						f[c] += basis * weight[y*w+x] * linear[y*w+x][c]
					}
				}
			}
			for c := range 3 {
				f[c] /= opaque
			}
			factors = append(factors, f)
		}
	}
	out := base83(cx-1+(cy-1)*9, 1, nil)
	maximum := 1.0
	if len(factors) > 1 {
		var largest float64
		for _, f := range factors[1:] {
			largest = max(largest, math.Abs(f[0]), math.Abs(f[1]), math.Abs(f[2]))
		}
		q := int(max(0, min(82, math.Floor(largest*166-0.5))))
		maximum = float64(q+1) / 166
		out = base83(q, 1, out)
	} else {
		out = base83(0, 1, out)
	}
	dc := factors[0]
	out = base83(toSRGB(dc[0])<<16+toSRGB(dc[1])<<8+toSRGB(dc[2]), 4, out)
	for _, f := range factors[1:] {
		var v int
		for _, c := range f {
			v = v*19 + int(max(0, min(18, math.Floor(signPow(c/maximum, 0.5)*9+9.5))))
		}
		out = base83(v, 2, out)
	}
	return string(out)
}

func toLinear(v uint8) float64 {
	f := float64(v) / 255
	if f <= 0.04045 {
		return f / 12.92
	}
	return math.Pow((f+0.055)/1.055, 2.4)
}

func toSRGB(v float64) int {
	v = max(0, min(1, v))
	if v <= 0.0031308 {
		return int(v*12.92*255 + 0.5)
	}
	return int((1.055*math.Pow(v, 1/2.4)-0.055)*255 + 0.5)
}

func signPow(v, exp float64) float64 {
	return math.Copysign(math.Pow(math.Abs(v), exp), v)
}

const digits83 = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz#$%*+,-.:;=?@[]^_{|}~"

func base83(v, length int, out []byte) []byte {
	for n := length - 1; n >= 0; n-- {
		out = append(out, digits83[v/int(math.Pow(83, float64(n)))%83])
	}
	return out
}
