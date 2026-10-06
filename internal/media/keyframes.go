package media

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"math"
	"os"
	"slices"
	"strconv"
	"strings"
)

// Keyframes lists the presentation times, in milliseconds, of the first video stream's keyframes.
// ffprobe reads packet flags without decoding, which is what the remux needs to cut segments at
// keyframes.
func (t Tools) Keyframes(ctx context.Context, f *os.File) ([]int64, error) {
	out, err := output(ctx, WholeRun(f), []*os.File{f}, t.FFprobe.Path,
		"-hide_banner", "-v", "error", "-protocol_whitelist", "fd", "-fd", "3",
		"-select_streams", "v:0", "-show_entries", "packet=pts_time,flags", "-of", "csv=p=0", "-i", "fd:")
	if err != nil {
		return nil, fmt.Errorf("ffprobe: %w", err)
	}
	return parseKeyframes(out)
}

func parseKeyframes(out []byte) ([]int64, error) {
	var pts []int64
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		at, flags, ok := strings.Cut(sc.Text(), ",")
		if !ok || !strings.HasPrefix(flags, "K") || at == "N/A" {
			continue
		}
		s, err := strconv.ParseFloat(at, 64)
		if err != nil {
			return nil, fmt.Errorf("keyframe time %q: %w", at, err)
		}
		pts = append(pts, int64(math.Round(s*1000)))
	}
	// Packets come in decode order; a keyframe's presentation time is not always later than the
	// one before it in that order.
	slices.Sort(pts)
	return pts, sc.Err()
}
