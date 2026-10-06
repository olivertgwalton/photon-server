package media

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// Keyframes lists the presentation times, in milliseconds, of the first video stream's keyframes,
// found as a library asks: from the container's own index, as Jellyfin reads a Matroska file's
// Cues, which costs a few reads however large the file; under KeyframesFull, a file with no index
// is walked whole with ffprobe. A file it finds none for answers an empty list, and is planned
// without them.
func (t Tools) Keyframes(ctx context.Context, f *os.File, mode domain.KeyframeMode) ([]int64, error) {
	switch mode {
	case domain.KeyframesOff:
		return nil, nil
	case domain.KeyframesIndex, domain.KeyframesFull:
	}
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	pts, err := indexedKeyframes(io.NewSectionReader(f, 0, info.Size()))
	switch {
	case err == nil:
		return pts, nil
	case !errors.Is(err, errNoIndex):
		return nil, err
	case mode == domain.KeyframesIndex:
		return nil, nil
	}
	return t.walkKeyframes(ctx, f)
}

// walkKeyframes reads every packet's flags with ffprobe, without decoding: the whole file.
func (t Tools) walkKeyframes(ctx context.Context, f *os.File) ([]int64, error) {
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

// errNoIndex is a file whose container keeps no usable keyframe index: none at all, or one cut
// short or malformed.
var errNoIndex = errors.New("no keyframe index")

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
		return nil, fmt.Errorf("%w: not Matroska or MP4", errNoIndex)
	}
	if err != nil {
		return nil, err
	}
	if len(pts) == 0 {
		return nil, fmt.Errorf("%w: an empty index", errNoIndex)
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
		return fmt.Errorf("%w: the file ends at %d", errNoIndex, off+int64(n))
	}
	return err
}

// readWhole reads size bytes at off, refusing more than maxIndex or than the file holds.
func readWhole(r *io.SectionReader, off, size int64) ([]byte, error) {
	if size < 0 || size > maxIndex || off+size > r.Size() {
		return nil, fmt.Errorf("%w: an index of %d bytes", errNoIndex, size)
	}
	b := make([]byte, size)
	return b, readAt(r, b, off)
}

// millis converts a time in units of scale per second to milliseconds, rounded.
func millis(t int64, scale uint32) int64 {
	return int64(math.Round(float64(t) * 1000 / float64(scale)))
}
