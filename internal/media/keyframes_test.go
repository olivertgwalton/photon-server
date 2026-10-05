package media

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
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
	if diff := cmp.Diff([]int64{0, 2000, 4000, 6000, 8000}, got); diff != "" {
		t.Errorf("keyframes (-want +got):\n%s", diff)
	}
}
