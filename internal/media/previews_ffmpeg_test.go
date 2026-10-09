package media

import (
	"cmp"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// A still past a video's last frame is no picture, not mjpeg's complaint about a limited-range
// picture it was never given, and a still within it is one.
func TestAStillPastTheEndIsNoPicture(t *testing.T) {
	path, err := exec.LookPath(cmp.Or(os.Getenv("PHOTON_FFMPEG"), "ffmpeg"))
	if err != nil {
		t.Skipf("needs ffmpeg: %v", err)
	}
	tools := Tools{FFmpeg: Tool{Path: path}}
	dir := t.TempDir()
	video := filepath.Join(dir, "limited.mp4")
	if out, err := exec.CommandContext(t.Context(), path, "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "testsrc2=s=320x180:r=25:d=2", "-g", "5", "-pix_fmt", "yuv420p", "-color_range", "tv",
		video).CombinedOutput(); err != nil {
		t.Fatalf("making the video: %v: %s", err, out)
	}
	f, err := os.Open(video)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	for _, decode := range []Decode{DecodeKeyframes, DecodeEvery} {
		within := filepath.Join(dir, string(decode)+"-within.jpg")
		if err := tools.Still(t.Context(), Input{File: f}, decode, time.Second, 160, domain.RangeSDR, within); err != nil {
			t.Fatalf("a still within the video from %s: %v", decode, err)
		}
		err = tools.Still(t.Context(), Input{File: f}, decode, time.Minute, 160, domain.RangeSDR, filepath.Join(dir, string(decode)+"-past.jpg"))
		if err == nil || strings.Contains(err.Error(), "full-range") {
			t.Errorf("a still past the end from %s: %v, want no picture", decode, err)
		}
	}
}
