package media

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Shade is what a frame shown at At looks like, as Intro Skipper reads credits: how much of it is
// black, in percent; its contrast, from its darkest tenth to its brightest pixel, which lettering
// on black has; and how saturated it is on average.
type Shade struct {
	At         time.Duration
	Black      int
	Contrast   int
	Saturation float64
}

// Shades are what the keyframes of the file's first video track look like from from to its end.
// Only keyframes are decoded, so the end of a film is read in seconds; each is made small before it
// is measured. A pixel is black at most a luma of 28, as Intro Skipper's. A file with a keyframe
// every second is most of its bytes to read over a network mount, so it may take a whole file's
// time.
func (t Tools) Shades(ctx context.Context, in Input, from time.Duration) ([]Shade, error) {
	args := []string{"-hide_banner", "-v", "error", "-skip_frame", "nokey", "-ss", strconv.FormatFloat(from.Seconds(), 'f', 3, 64), "-copyts"}
	out, err := output(ctx, Background, in.WholeRun(), t.FFmpeg.Path, slices.Concat(args, in.Args(), []string{
		"-map", "0:v:0", "-vf", "scale=320:-2,blackframe=amount=0:threshold=28,signalstats,metadata=print:file=-",
		"-f", "null", "-",
	})...)
	if err != nil {
		return nil, fmt.Errorf("ffmpeg shades: %w", err)
	}
	return parseShades(out)
}

// parseShades reads metadata's print of each frame: its time, then its measures, a line each.
func parseShades(out []byte) ([]Shade, error) {
	var shades []Shade
	var low, high int
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		line := sc.Text()
		if _, t, ok := strings.Cut(line, "pts_time:"); ok {
			s, err := strconv.ParseFloat(strings.Fields(t)[0], 64)
			if err != nil {
				return nil, fmt.Errorf("frame time %q: %w", t, err)
			}
			shades = append(shades, Shade{At: time.Duration(s * float64(time.Second))})
			continue
		}
		key, value, ok := strings.Cut(strings.TrimPrefix(line, "lavfi."), "=")
		if !ok || len(shades) == 0 {
			continue
		}
		v, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return nil, fmt.Errorf("%s %q: %w", key, value, err)
		}
		s := &shades[len(shades)-1]
		switch key {
		case "blackframe.pblack":
			s.Black = int(v)
		case "signalstats.YLOW":
			low = int(v)
		case "signalstats.YMAX":
			high = int(v)
		case "signalstats.SATAVG":
			s.Saturation = v
		}
		s.Contrast = high - low
	}
	return shades, sc.Err()
}
