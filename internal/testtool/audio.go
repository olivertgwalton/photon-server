package testtool

import (
	"cmp"
	"os"
	"os/exec"
	"testing"

	"go.senan.xyz/taglib"
)

// Audio writes a second of silence to path, in the format its extension names, tagged with tags
// and holding cover as its front cover where it is given. The test is skipped without ffmpeg.
func Audio(tb testing.TB, path string, tags map[string][]string, cover []byte) {
	tb.Helper()
	ffmpeg, err := exec.LookPath(cmp.Or(os.Getenv("PHOTON_FFMPEG"), "ffmpeg"))
	if err != nil {
		tb.Skipf("needs ffmpeg: %v", err)
	}
	if out, err := exec.CommandContext(tb.Context(), ffmpeg, "-hide_banner", "-loglevel", "error", "-y", //nolint:gosec // the test's own ffmpeg, writing where the test says
		"-f", "lavfi", "-i", "anullsrc=r=44100:cl=stereo", "-t", "1", path).CombinedOutput(); err != nil {
		tb.Fatalf("making %s: %v: %s", path, err, out)
	}
	if err := taglib.WriteTags(path, tags, taglib.Clear); err != nil {
		tb.Fatal(err)
	}
	if cover != nil {
		if err := taglib.WriteImage(path, cover); err != nil {
			tb.Fatal(err)
		}
	}
}
