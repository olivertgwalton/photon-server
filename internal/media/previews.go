package media

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ToneMap maps an HDR picture to SDR in software: jellyfin-ffmpeg's tonemapx, as Jellyfin's
// software transcode does.
const ToneMap = "tonemapx=tonemap=bt2390:desat=0:peak=100:t=bt709:m=bt709:p=bt709:format=yuv420p"

// Grid is how trickplay thumbnails are taken and laid out: one every Interval, Width pixels wide,
// Columns by Rows to a sheet.
type Grid struct {
	Interval time.Duration
	Width    int
	Columns  int
	Rows     int
}

// Thumbnails is what a trickplay run made: each thumbnail's size, and how many there are.
type Thumbnails struct {
	Width, Height, Count int
}

// jpegQuality is ffmpeg's -q:v for a picture: 2 is best, 31 worst.
const jpegQuality = "3"

// fitted scales a picture to width, keeping its displayed shape and an even height, with square
// pixels, then maps HDR to SDR.
func fitted(width int, toneMap bool) string {
	f := "scale=" + strconv.Itoa(width) + ":trunc(ow/dar/2)*2,setsar=1"
	if toneMap {
		f += "," + ToneMap
	}
	return f
}

// Trickplay writes a video's thumbnail sheets into dir as 0.jpg, 1.jpg… in one run, decoding
// keyframes only, as Jellyfin's keyframe-only extraction and Plex's index do: each thumbnail is the
// keyframe nearest its time. A second output lists the thumbnails, which is how many there are.
func (t Tools) Trickplay(ctx context.Context, f *os.File, dir string, g Grid, toneMap bool) (Thumbnails, error) {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return Thumbnails{}, err
	}
	graph := fmt.Sprintf("[0:v:0]fps=1000/%d,%s,split[s][n];[s]tile=%dx%d[t]",
		g.Interval.Milliseconds(), fitted(g.Width, toneMap), g.Columns, g.Rows)
	out, err := output(ctx, wholeRun(f), []*os.File{f}, t.FFmpeg.Path,
		"-hide_banner", "-loglevel", "error", "-nostdin", "-skip_frame", "nokey",
		"-protocol_whitelist", "fd", "-fd", "3", "-i", "fd:", "-an", "-sn", "-dn", "-filter_complex", graph,
		"-map", "[t]", "-c:v", "mjpeg", "-q:v", jpegQuality, "-f", "image2", "-start_number", "0",
		filepath.Join(dir, "%d.jpg"),
		"-map", "[n]", "-f", "framecrc", "pipe:1")
	if err != nil {
		return Thumbnails{}, fmt.Errorf("ffmpeg: %w", err)
	}
	return countFrames(out)
}

// countFrames reads framecrc's listing: a header giving the size, then a line per frame.
func countFrames(out []byte) (Thumbnails, error) {
	var th Thumbnails
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		line := sc.Text()
		if size, ok := strings.CutPrefix(line, "#dimensions 0: "); ok {
			w, h, _ := strings.Cut(size, "x")
			th.Width, _ = strconv.Atoi(w)
			th.Height, _ = strconv.Atoi(h)
		} else if line != "" && !strings.HasPrefix(line, "#") {
			th.Count++
		}
	}
	if th.Width == 0 || th.Height == 0 || th.Count == 0 {
		return th, fmt.Errorf("ffmpeg made no thumbnails: %q", out)
	}
	return th, sc.Err()
}

// Still writes the picture shown at a time in a video to path as a JPEG, width pixels wide.
func (t Tools) Still(ctx context.Context, f *os.File, at time.Duration, width int, toneMap bool, path string) error {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	_, err := output(ctx, partRun, []*os.File{f}, t.FFmpeg.Path,
		"-hide_banner", "-loglevel", "error", "-nostdin", "-ss", strconv.FormatFloat(at.Seconds(), 'f', 3, 64),
		"-protocol_whitelist", "fd", "-fd", "3", "-i", "fd:", "-an", "-sn", "-dn", "-frames:v", "1",
		"-vf", fitted(width, toneMap), "-c:v", "mjpeg", "-q:v", jpegQuality, "-f", "image2", "-update", "1", path)
	if err != nil {
		return fmt.Errorf("ffmpeg: %w", err)
	}
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("ffmpeg made no picture at %s: %w", at, err)
	}
	return nil
}
