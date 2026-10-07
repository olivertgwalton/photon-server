package media

import (
	"cmp"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	gocmp "github.com/google/go-cmp/cmp"
)

// A file's fonts are found whatever they are named, and whether or not their type says they are
// fonts; a picture it carries is none.
func TestFontsAreFoundByKindOrName(t *testing.T) {
	tools := ffprobe(t)
	ffmpeg, err := exec.LookPath(cmp.Or(os.Getenv("PHOTON_FFMPEG"), "ffmpeg"))
	if err != nil {
		t.Skipf("needs ffmpeg: %v", err)
	}
	dir := t.TempDir()
	for _, name := range []string{"Shop Sans.ttf", "OTHER.OTF", "cover.jpg"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("bytes"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	film := filepath.Join(dir, "film.mkv")
	made := exec.CommandContext(t.Context(), ffmpeg, "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "testsrc2=size=64x48:rate=24", "-t", "1",
		"-attach", filepath.Join(dir, "Shop Sans.ttf"), "-metadata:s:t:0", "mimetype=application/x-truetype-font",
		"-attach", filepath.Join(dir, "OTHER.OTF"), "-metadata:s:t:1", "mimetype=application/octet-stream",
		"-attach", filepath.Join(dir, "cover.jpg"), "-metadata:s:t:2", "mimetype=image/jpeg",
		"-c:v", "libx264", film)
	if out, err := made.CombinedOutput(); err != nil {
		t.Fatalf("making the film: %v: %s", err, out)
	}
	f, err := os.Open(film)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	got, err := tools.Fonts(t.Context(), f)
	if err != nil {
		t.Fatal(err)
	}
	if diff := gocmp.Diff([]Font{{Index: 1, Ext: ".ttf"}, {Index: 2, Ext: ".otf"}}, got); diff != "" {
		t.Errorf("fonts (-want +got):\n%s", diff)
	}
}
