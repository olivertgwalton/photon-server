package media

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"
)

// ErrNoChromaprint is the answer of an FFmpeg built without the chromaprint muxer.
var ErrNoChromaprint = errors.New("ffmpeg has no chromaprint muxer")

// hasChromaprint reports whether ffmpeg can write a fingerprint: Homebrew's cannot, jellyfin-ffmpeg's can.
func hasChromaprint(ctx context.Context, ffmpeg string) bool {
	out, err := output(ctx, nil, ffmpeg, "-hide_banner", "-h", "muxer=chromaprint")
	return err == nil && bytes.Contains(out, []byte("Muxer chromaprint"))
}

// Fingerprint is the chromaprint of length of the file's first audio track from from, as
// Intro Skipper takes it: one 32-bit point per 4096/33075 of a second.
func (t Tools) Fingerprint(ctx context.Context, f *os.File, from, length time.Duration) ([]uint32, error) {
	if !t.Chromaprint {
		return nil, ErrNoChromaprint
	}
	out, err := output(ctx, []*os.File{f}, t.FFmpeg.Path,
		"-hide_banner", "-v", "error", "-protocol_whitelist", "fd", "-fd", "3",
		"-ss", strconv.FormatFloat(from.Seconds(), 'f', 3, 64), "-t", strconv.FormatFloat(length.Seconds(), 'f', 3, 64), "-i", "fd:",
		"-map", "0:a:0", "-ac", "2", "-f", "chromaprint", "-fp_format", "raw", "-")
	if err != nil {
		return nil, fmt.Errorf("ffmpeg chromaprint: %w", err)
	}
	points := make([]uint32, len(out)/4)
	for i := range points {
		points[i] = binary.LittleEndian.Uint32(out[4*i:])
	}
	return points, nil
}
