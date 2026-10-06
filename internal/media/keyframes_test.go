package media

import (
	"bytes"
	"cmp"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	gocmp "github.com/google/go-cmp/cmp"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// keyframes.csv is ffprobe 9.0.1's packet list for ten seconds of 24 fps H.264 with a keyframe
// every 48 frames and B-frames, so packets arrive out of presentation order.
func TestKeyframes(t *testing.T) {
	out, err := os.ReadFile(filepath.Join("testdata", "keyframes.csv"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := parseKeyframes(out)
	if err != nil {
		t.Fatal(err)
	}
	if diff := gocmp.Diff([]int64{0, 2000, 4000, 6000, 8000}, got); diff != "" {
		t.Errorf("keyframes (-want +got):\n%s", diff)
	}
}

// indexed are files FFmpeg 9 wrote of eight seconds of 24000/1001 fps video (see
// testdata/keyframes/make.sh), and the keyframes ffprobe's packet walk finds in each. The H.264 has
// B-frames and keyframes forced at uneven times.
var indexed = map[string][]int64{
	// Cues after the clusters, found through the SeekHead, with an audio track's cues beside.
	"cues-end.mkv": {0, 1335, 2002, 4713, 5130, 7257},
	// Cues in space reserved before the clusters.
	"cues-front.mkv": {0, 1335, 2002, 4713, 5130, 7257},
	"vp9.webm":       {0, 1251, 2503, 3754, 5005, 6256, 7508},
	// The moov after the mdat, the video the second track, an empty edit of 1.5 s and the
	// B-frames' delay edited out.
	"moov-end.mp4":     {1500, 2835, 3502, 6213, 6630, 8757},
	"faststart.mov":    {0, 1335, 2002, 4713, 5130, 7257},
	"negative-cts.mp4": {0, 1335, 2002, 4713, 5130, 7257},
	// No stss: every picture is a keyframe.
	"intra.mp4": {0, 200, 400, 600, 800, 1000, 1200, 1400, 1600, 1800, 2000, 2200, 2400, 2600, 2800},
	// A fragment per keyframe, listed in the mfra.
	"fragmented.mp4": {83, 1418, 2085, 4796, 5214, 7341},
}

// unindexed are files whose container lists no keyframes: Matroska written to a pipe, which cannot
// go back to write Cues, and MPEG-TS, which has no index.
var unindexed = []string{"no-cues.mkv", "transport.ts"}

func fixture(t *testing.T, name string) *os.File {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", "keyframes", name))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

// counting is a file that counts the bytes read from it.
type counting struct {
	r    io.ReaderAt
	read int64
}

func (c *counting) ReadAt(p []byte, off int64) (int, error) {
	n, err := c.r.ReadAt(p, off)
	c.read += int64(n)
	return n, err
}

func TestKeyframesAreReadFromTheContainersIndex(t *testing.T) {
	for name, want := range indexed {
		t.Run(name, func(t *testing.T) {
			f := fixture(t, name)
			info, err := f.Stat()
			if err != nil {
				t.Fatal(err)
			}
			c := &counting{r: f}
			got, err := indexedKeyframes(io.NewSectionReader(c, 0, info.Size()))
			if err != nil {
				t.Fatal(err)
			}
			if diff := gocmp.Diff(want, got); diff != "" {
				t.Errorf("keyframes (-want +got):\n%s", diff)
			}
			if c.read > info.Size()/4 {
				t.Errorf("read %d of %d bytes", c.read, info.Size())
			}
		})
	}
	for _, name := range unindexed {
		f := fixture(t, name)
		info, err := f.Stat()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := indexedKeyframes(io.NewSectionReader(f, 0, info.Size())); !errors.Is(err, errNoIndex) {
			t.Errorf("%s: %v, want no index", name, err)
		}
	}
}

// ffprobe finds ffprobe, or skips: CI has none.
func ffprobe(t *testing.T) Tools {
	t.Helper()
	path, err := exec.LookPath(cmp.Or(os.Getenv("PHOTON_FFPROBE"), "ffprobe"))
	if err != nil {
		t.Skipf("needs ffprobe: %v", err)
	}
	return Tools{FFprobe: Tool{Path: path}}
}

// The fixtures' keyframes above are what ffprobe walks to, to the millisecond.
func TestIndexedKeyframesAreFFprobes(t *testing.T) {
	tools := ffprobe(t)
	for name, want := range indexed {
		got, err := tools.walkKeyframes(t.Context(), fixture(t, name))
		if err != nil {
			t.Fatal(err)
		}
		if diff := gocmp.Diff(want, got); diff != "" {
			t.Errorf("%s: ffprobe's keyframes (-index +ffprobe):\n%s", name, diff)
		}
	}
}

func TestEachModeFindsKeyframesAsItSays(t *testing.T) {
	tools := ffprobe(t)
	cases := []struct {
		mode domain.KeyframeMode
		file string
		want []int64
	}{
		{domain.KeyframesIndex, "cues-end.mkv", indexed["cues-end.mkv"]},
		{domain.KeyframesIndex, "transport.ts", nil},
		{domain.KeyframesFull, "cues-end.mkv", indexed["cues-end.mkv"]},
		{domain.KeyframesFull, "transport.ts", []int64{1483, 2818, 3485, 6196, 6614, 8741}},
		{domain.KeyframesFull, "no-cues.mkv", []int64{0, 1335, 2002, 4713, 5130, 7257}},
		{domain.KeyframesOff, "cues-end.mkv", nil},
	}
	for _, c := range cases {
		got, err := tools.Keyframes(t.Context(), fixture(t, c.file), c.mode)
		if err != nil {
			t.Fatalf("%s, %s: %v", c.mode, c.file, err)
		}
		if diff := gocmp.Diff(c.want, got); diff != "" {
			t.Errorf("%s, %s (-want +got):\n%s", c.mode, c.file, diff)
		}
	}
}

// A file cut short anywhere answers its whole index, where that is before the cut, or no index; one
// damaged anywhere never panics.
func TestADamagedIndexIsRefused(t *testing.T) {
	for name, want := range indexed {
		data, err := os.ReadFile(filepath.Join("testdata", "keyframes", name))
		if err != nil {
			t.Fatal(err)
		}
		for cut := 0; cut < len(data); cut += max(1, len(data)/500) {
			got, err := indexedKeyframes(io.NewSectionReader(bytes.NewReader(data[:cut]), 0, int64(cut)))
			if err != nil && !errors.Is(err, errNoIndex) || err == nil && !slices.Equal(got, want) {
				t.Errorf("%s cut at %d: %v, %v", name, cut, got, err)
			}
			damaged := bytes.Clone(data)
			damaged[cut] ^= 0xff
			_, _ = indexedKeyframes(io.NewSectionReader(bytes.NewReader(damaged), 0, int64(len(damaged))))
		}
	}
}

func FuzzIndexedKeyframes(f *testing.F) {
	for name := range indexed {
		data, err := os.ReadFile(filepath.Join("testdata", "keyframes", name))
		if err != nil {
			f.Fatal(err)
		}
		f.Add(data)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		c := &counting{r: bytes.NewReader(data)}
		_, err := indexedKeyframes(io.NewSectionReader(c, 0, int64(len(data))))
		if err != nil && !errors.Is(err, errNoIndex) {
			t.Errorf("an error that is not the index's: %v", err)
		}
	})
}
