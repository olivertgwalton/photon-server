package media

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/olivertgwalton/photon-server/internal/domain"
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
// pixels, then maps a source's HDR to SDR.
func fitted(width int, source domain.Range) string {
	f := "scale=" + strconv.Itoa(width) + ":trunc(ow/dar/2)*2,setsar=1"
	if source.HDR() {
		f += "," + ToneMap
	}
	// JPEG is full range. Stated, an encoder opened with no frame (a time past the last one)
	// fails as having none instead of with mjpeg's misleading complaint about limited range.
	return f + ",format=yuvj420p"
}

// trickplayThreads is how many threads the trickplay ffmpeg decodes, filters and encodes its sheets
// with: one, Jellyfin's default for its trickplay's, so a library's sheets never take more than a
// core from a playback.
const trickplayThreads = 1

// Trickplay writes a video's thumbnail sheets into dir as 0.jpg, 1.jpg… in one run, decoding
// keyframes only, as Jellyfin's keyframe-only extraction and Plex's index do: each thumbnail is the
// keyframe nearest its time. A second output lists the thumbnails, which is how many there are.
func (t Tools) Trickplay(ctx context.Context, in Input, dir string, g Grid, source domain.Range) (Thumbnails, error) {
	if _, err := in.File.Seek(0, io.SeekStart); err != nil {
		return Thumbnails{}, err
	}
	graph := fmt.Sprintf("[0:v:0]fps=1000/%d,%s,split[s][n];[s]tile=%dx%d[t]",
		g.Interval.Milliseconds(), fitted(g.Width, source), g.Columns, g.Rows)
	args := []string{"-hide_banner", "-loglevel", "error", "-nostdin", "-skip_frame", "nokey", "-threads", strconv.Itoa(trickplayThreads)}
	out, err := output(ctx, Background, in.WholeRun(), in.Files(), t.FFmpeg.Path, slices.Concat(args, in.Args(), []string{
		"-an", "-sn", "-dn",
		"-filter_complex_threads", strconv.Itoa(trickplayThreads), "-filter_complex", graph,
		"-map", "[t]", "-threads", strconv.Itoa(trickplayThreads), "-c:v", "mjpeg", "-q:v", jpegQuality, "-f", "image2", "-start_number", "0",
		filepath.Join(dir, "%d.jpg"),
		"-map", "[n]", "-f", "framecrc", "pipe:1",
	})...)
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
			var werr, herr error
			th.Width, werr = strconv.Atoi(w)
			th.Height, herr = strconv.Atoi(h)
			if err := errors.Join(werr, herr); err != nil {
				return th, fmt.Errorf("ffmpeg's thumbnail size %q: %w", size, err)
			}
		} else if line != "" && !strings.HasPrefix(line, "#") {
			th.Count++
		}
	}
	if th.Width == 0 || th.Height == 0 || th.Count == 0 {
		return th, fmt.Errorf("ffmpeg made no thumbnails: %q", out)
	}
	return th, sc.Err()
}

// Decode is which of a video's frames a still is taken from.
type Decode string

const (
	// DecodeKeyframes takes the keyframe at or after the time, decoding nothing else.
	DecodeKeyframes Decode = "keyframes"
	// DecodeEvery takes the frame at the time, decoding every frame from the keyframe before it.
	DecodeEvery Decode = "every"
)

// Still writes the frame at a time in a video, taken as decode says, to path as a JPEG, width
// pixels wide.
func (t Tools) Still(ctx context.Context, in Input, decode Decode, at time.Duration, width int, source domain.Range, path string) error {
	if _, err := in.File.Seek(0, io.SeekStart); err != nil {
		return err
	}
	args := []string{"-hide_banner", "-loglevel", "error", "-nostdin"}
	switch decode {
	case DecodeKeyframes:
		args = append(args, "-skip_frame", "nokey")
	case DecodeEvery:
	}
	args = append(args, "-ss", strconv.FormatFloat(at.Seconds(), 'f', 3, 64))
	_, err := output(ctx, Background, PartRun, in.Files(), t.FFmpeg.Path, slices.Concat(args, in.Args(), []string{
		"-an", "-sn", "-dn", "-frames:v", "1",
		"-vf", fitted(width, source), "-c:v", "mjpeg", "-q:v", jpegQuality, "-f", "image2", "-update", "1", path,
	})...)
	if err != nil {
		return fmt.Errorf("ffmpeg: %w", err)
	}
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("ffmpeg made no picture at %s: %w", at, err)
	}
	return nil
}
