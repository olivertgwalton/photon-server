package library

import (
	"errors"
	"io/fs"
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
	got := map[string]Folder{}
	for f, err := range Walk(dir) {
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

func TestSubsFoldersBelongToTheirParent(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, map[string]string{
		"Heat (1995)/Heat (1995).mkv":              "v",
		"Heat (1995)/Subs/English.srt":             "s",
		"Heat (1995)/Subtitles/Heat (1995).fr.srt": "s",
		"Heat (1995)/Subs/notes.txt":               "x",
	})
	got := walkAll(t, dir)
	if _, separate := got["Heat (1995)/Subs"]; separate {
		t.Error("the Subs folder was walked as a folder of its own")
	}
	want := []string{"Heat (1995).mkv", "Subs/English.srt", "Subtitles/Heat (1995).fr.srt"}
	if diff := cmp.Diff(want, fileNames(got["Heat (1995)"])); diff != "" {
		t.Errorf("Heat's files (-want +got):\n%s", diff)
	}
	before := got["Heat (1995)"].Fingerprint
	write(t, dir, map[string]string{"Heat (1995)/Subs/German.srt": "s"})
	if walkAll(t, dir)["Heat (1995)"].Fingerprint == before {
		t.Error("adding a subtitle under Subs did not change the film folder's fingerprint")
	}
}

// A library of links into another mount, as debrid and *arr setups make them, reads as the files
// the links lead to; a link that leads nowhere, or back up, is left out and says why.
func TestWalkFollowsLinksOutOfTheLibrary(t *testing.T) {
	dir, mount := t.TempDir(), t.TempDir()
	write(t, mount, map[string]string{
		"films/Heat.1995.2160p.mkv":     "v",
		"shows/Severance/S01E01.mkv":    "v",
		"shows/Severance/S01E01.en.srt": "s",
	})
	link := func(target, name string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	link(filepath.Join(mount, "films/Heat.1995.2160p.mkv"), "Heat (1995)/Heat (1995).mkv")
	link(filepath.Join(mount, "gone.mkv"), "Heat (1995)/Heat (1995) - 1080p.mkv")
	link(filepath.Join(mount, "shows/Severance"), "Severance")
	link(dir, "Heat (1995)/back up")

	got := walkAll(t, dir)
	if diff := cmp.Diff([]string{".", "Heat (1995)", "Severance"}, slices.Sorted(maps.Keys(got))); diff != "" {
		t.Errorf("folders (-want +got):\n%s", diff)
	}
	heat := got["Heat (1995)"]
	if diff := cmp.Diff([]string{"Heat (1995).mkv"}, fileNames(heat)); diff != "" {
		t.Errorf("Heat's files (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]string{"S01E01.en.srt", "S01E01.mkv"}, fileNames(got["Severance"])); diff != "" {
		t.Errorf("Severance's files (-want +got):\n%s", diff)
	}
	skipped := map[string]error{}
	for _, s := range heat.Skipped {
		skipped[s.Name] = s.Err
	}
	if err := skipped["Heat (1995) - 1080p.mkv"]; !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("a link to nothing: %v, want it left out as not there", err)
	}
	if err := skipped["back up"]; !errors.Is(err, errLoop) {
		t.Errorf("a link to the library: %v, want it left out as a loop", err)
	}

	f, err := Open(dir, "Severance/S01E01.mkv")
	if err != nil {
		t.Fatalf("a file through a linked folder: %v", err)
	}
	f.Close()
	if _, err := Open(dir, "../"+filepath.Base(mount)+"/films/Heat.1995.2160p.mkv"); !errors.Is(err, errNotInside) {
		t.Errorf("a path climbing out of the library: %v, want it refused", err)
	}
}
