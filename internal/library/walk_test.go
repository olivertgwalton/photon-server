package library

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

func write(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func walkAll(t *testing.T, dir string) map[string]Folder {
	t.Helper()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	got := map[string]Folder{}
	for f, err := range Walk(root) {
		if err != nil {
			t.Fatal(err)
		}
		got[f.Path] = f
	}
	return got
}

func fileNames(f Folder) []string {
	var names []string
	for _, file := range f.Files {
		names = append(names, file.Name)
	}
	return names
}

func TestWalkSkipsWhatIsNotALibrary(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, map[string]string{
		"Heat (1995)/Heat (1995).mkv":            "v",
		"Heat (1995)/.DS_Store":                  "x",
		"Heat (1995)/@eaDir/Heat (1995).mkv":     "thumb",
		"Private/.ignore":                        "",
		"Private/Home Video.mkv":                 "v",
		"Alien (1979)/Alien (1979).mkv":          "v",
		"Alien (1979)/Alien (1979).en.srt":       "s",
		"Alien (1979)/Featurettes/Making Of.mp4": "v",
	})
	outside := filepath.Join(t.TempDir(), "elsewhere.mkv")
	write(t, filepath.Dir(outside), map[string]string{"elsewhere.mkv": "v"})
	if err := os.Symlink(outside, filepath.Join(dir, "Heat (1995)", "escape.mkv")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "Alien (1979)"), filepath.Join(dir, "loop")); err != nil {
		t.Fatal(err)
	}

	got := walkAll(t, dir)
	want := []string{".", "Alien (1979)", "Alien (1979)/Featurettes", "Heat (1995)"}
	if diff := cmp.Diff(want, slices.Sorted(maps.Keys(got))); diff != "" {
		t.Errorf("folders (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]string{"Heat (1995).mkv"}, fileNames(got["Heat (1995)"])); diff != "" {
		t.Errorf("Heat's files (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]string{"Alien (1979).en.srt", "Alien (1979).mkv"}, fileNames(got["Alien (1979)"])); diff != "" {
		t.Errorf("Alien's files (-want +got):\n%s", diff)
	}
}

func TestFingerprintMovesOnlyWithItsFolder(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, map[string]string{
		"Heat (1995)/Heat (1995).mkv":   "v",
		"Alien (1979)/Alien (1979).mkv": "v",
	})
	before := walkAll(t, dir)
	if again := walkAll(t, dir); again["Heat (1995)"].Fingerprint != before["Heat (1995)"].Fingerprint {
		t.Fatal("an untouched folder's fingerprint changed")
	}

	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(filepath.Join(dir, "Heat (1995)", "Heat (1995).mkv"), later, later); err != nil {
		t.Fatal(err)
	}
	after := walkAll(t, dir)
	if after["Heat (1995)"].Fingerprint == before["Heat (1995)"].Fingerprint {
		t.Error("rewriting a file did not change its folder's fingerprint")
	}
	if after["Alien (1979)"].Fingerprint != before["Alien (1979)"].Fingerprint {
		t.Error("a sibling folder's fingerprint changed")
	}

	write(t, dir, map[string]string{"Heat (1995)/Heat (1995).en.srt": "s"})
	if walkAll(t, dir)["Heat (1995)"].Fingerprint == after["Heat (1995)"].Fingerprint {
		t.Error("adding a file did not change its folder's fingerprint")
	}
}
