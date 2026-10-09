package media

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"strconv"
	"strings"
)

// IndexedKeyframes lists the presentation times, in milliseconds, of the first video stream's
// keyframes that a Matroska or MP4 file lists in its own index, as Jellyfin reads a Matroska file's
// Cues: a few reads however large the file. ErrNoIndex for a file that keeps none.
func IndexedKeyframes(in Input) ([]int64, error) {
	info, err := in.File.Stat()
	if err != nil {
		return nil, err
	}
	return indexedKeyframes(io.NewSectionReader(in.File, 0, info.Size()))
}

// WalkKeyframes reads every packet's flags with ffprobe, without decoding: the whole file. A file
// it finds none in answers an empty list.
func (t Tools) WalkKeyframes(ctx context.Context, in Input) ([]int64, error) {
	out, err := output(ctx, Background, in.WholeRun(), in.Files(), t.FFprobe.Path, append([]string{
		"-hide_banner", "-v", "error", "-select_streams", "v:0", "-show_entries", "packet=pts_time,flags", "-of", "csv=p=0",
	}, in.Args()...)...)
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

// ErrNoIndex is a file whose container keeps no usable keyframe index: none at all, or one cut
// short or malformed.
var ErrNoIndex = errors.New("no keyframe index")

// maxIndex bounds one element or box of an index read whole: a three-hour film's composition
// offsets are a few megabytes.
const maxIndex = 64 << 20

// maxHeads bounds the elements or boxes walked past at one level looking for an index: a real file
// has a handful before its first cluster or fragment.
const maxHeads = 4096

// indexedKeyframes reads the keyframes a Matroska or MP4 file lists in its own index, sorted and
// without repeats.
func indexedKeyframes(r *io.SectionReader) ([]int64, error) {
	head := make([]byte, 8)
	if err := readAt(r, head, 0); err != nil {
		return nil, err
	}
	var pts []int64
	var err error
	switch {
	case bytes.HasPrefix(head, ebmlMagic):
		pts, err = matroskaKeyframes(r)
	case slices.Contains(isobmffStarts, string(head[4:8])):
		pts, err = mp4Keyframes(r)
	default:
		return nil, fmt.Errorf("%w: not Matroska or MP4", ErrNoIndex)
	}
	if err != nil {
		return nil, err
	}
	if len(pts) == 0 {
		return nil, fmt.Errorf("%w: an empty index", ErrNoIndex)
	}
	slices.Sort(pts)
	return slices.Compact(pts), nil
}

// readAt fills b from off. A file that ends first is an index cut short; any other failure is the
// disk's, and is returned as it is.
func readAt(r *io.SectionReader, b []byte, off int64) error {
	n, err := r.ReadAt(b, off)
	if n == len(b) {
		return nil
	}
	if err == nil || errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: the file ends at %d", ErrNoIndex, off+int64(n))
	}
	return err
}

// readWhole reads size bytes at off, refusing more than maxIndex or than the file holds.
func readWhole(r *io.SectionReader, off, size int64) ([]byte, error) {
	if size < 0 || size > maxIndex || off+size > r.Size() {
		return nil, fmt.Errorf("%w: an index of %d bytes", ErrNoIndex, size)
	}
	b := make([]byte, size)
	return b, readAt(r, b, off)
}

// millis converts a time in units of scale per second to milliseconds, rounded.
func millis(t int64, scale uint32) int64 {
	return int64(math.Round(float64(t) * 1000 / float64(scale)))
}
