package library

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sync"
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

// walkFrom walks folders of the library at root, answering every folder read.
func walkFrom(t *testing.T, root string, dirs ...string) map[string]Folder {
	t.Helper()
	var mu sync.Mutex
	got := map[string]Folder{}
	err := Walk(t.Context(), root, dirs, 4, func(_ context.Context, f Folder, err error) error {
		if err != nil {
			return err
		}
		mu.Lock()
		defer mu.Unlock()
		got[f.Path] = f
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func walkAll(t *testing.T, dir string) map[string]Folder {
	t.Helper()
	return walkFrom(t, dir, ".")
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

func TestAnIgnoreFileHidesWhatItsPatternsMatch(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, map[string]string{
		"Heat (1995)/.ignore":                   "# Kodi's files\n*.nfo\nExtras/\n!keep.nfo\n",
		"Heat (1995)/Heat (1995).mkv":           "v",
		"Heat (1995)/Heat (1995).nfo":           "n",
		"Heat (1995)/keep.nfo":                  "n",
		"Heat (1995)/Extras/Making Of.mkv":      "v",
		"Heat (1995)/Subs/English.nfo":          "n",
		"Heat (1995)/Subs/English.srt":          "s",
		"Heat (1995)/Deleted/Scene.mkv":         "v",
		"Heat (1995)/Deleted/Scene.nfo":         "n",
		"Heat (1995)/Deleted/Old/.ignore":       "*.mkv",
		"Heat (1995)/Deleted/Old/Take.mkv":      "v",
		"Heat (1995)/Deleted/Old/Take.nfo":      "n",
		"The Wire/.ignore":                      "/Season 2/\n**/sample.*",
		"The Wire/Season 1/S01E01.mkv":          "v",
		"The Wire/Season 1/sample.mkv":          "v",
		"The Wire/Season 2/S02E01.mkv":          "v",
		"The Wire/Specials/Season 2/S00E01.mkv": "v",
		"Private/.ignore":                       " \n\n",
		"Private/Home Video.mkv":                "v",
	})
	got := walkAll(t, dir)
	want := []string{
		".", "Heat (1995)", "Heat (1995)/Deleted", "Heat (1995)/Deleted/Old",
		"The Wire", "The Wire/Season 1", "The Wire/Specials", "The Wire/Specials/Season 2",
	}
	if diff := cmp.Diff(want, slices.Sorted(maps.Keys(got))); diff != "" {
		t.Errorf("folders (-want +got):\n%s", diff)
	}
	for folder, files := range map[string][]string{
		"Heat (1995)":             {"Heat (1995).mkv", "Subs/English.srt", "keep.nfo"},
		"Heat (1995)/Deleted":     {"Scene.mkv"},
		"Heat (1995)/Deleted/Old": {"Take.nfo"},
		"The Wire/Season 1":       {"S01E01.mkv"},
	} {
		if diff := cmp.Diff(files, fileNames(got[folder])); diff != "" {
			t.Errorf("%s's files (-want +got):\n%s", folder, diff)
		}
	}
}

func TestWalkingAFolderWalksWhatTheWholeWalkFindsThere(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, map[string]string{
		".ignore":                          "**/Season 2/\n",
		"The Wire/Season 1/E01.mkv":        "v",
		"The Wire/Season 1/Extras/Cut.mkv": "v",
		"The Wire/Season 2/E01.mkv":        "v",
		"Private/.ignore":                  "",
		"Private/Diary/Home Video.mkv":     "v",
	})
	whole := walkAll(t, dir)
	part := walkFrom(t, dir, "The Wire/Season 1")
	if diff := cmp.Diff([]string{"The Wire/Season 1", "The Wire/Season 1/Extras"}, slices.Sorted(maps.Keys(part))); diff != "" {
		t.Errorf("folders (-want +got):\n%s", diff)
	}
	if part["The Wire/Season 1"].Fingerprint != whole["The Wire/Season 1"].Fingerprint {
		t.Error("the folder walked alone has another fingerprint")
	}
	// A .ignore above the folder still hides it.
	for _, hidden := range []string{"The Wire/Season 2", "Private/Diary"} {
		for f := range walkFrom(t, dir, hidden) {
			t.Errorf("walking %s read %s", hidden, f)
		}
	}
}

func TestRemoveTakesAFileAndTheFoldersItEmpties(t *testing.T) {
	root := t.TempDir()
	write := func(rel string) {
		t.Helper()
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("Heat (1995)/Heat.mkv")
	write("Show/Season 1/e1.mkv")
	write("Show/Season 1/e2.mkv")
	write("Show/poster.jpg")

	if err := Remove(root, "Heat (1995)/Heat.mkv"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "Heat (1995)")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the film's emptied folder: %v, want it gone", err)
	}
	for _, rel := range []string{"Show/Season 1/e1.mkv", "Show/Season 1/e2.mkv"} {
		if err := Remove(root, rel); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "Show", "Season 1")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the emptied season folder: %v, want it gone", err)
	}
	if _, err := os.Stat(filepath.Join(root, "Show", "poster.jpg")); err != nil {
		t.Errorf("the show's folder, which holds its poster still: %v, want it kept", err)
	}
	if _, err := os.Stat(root); err != nil {
		t.Errorf("the library's root: %v, want it kept", err)
	}
	if err := Remove(root, "../outside.mkv"); !errors.Is(err, errNotInside) {
		t.Errorf("removing outside the library: %v, want %v", err, errNotInside)
	}
}

func TestAShowsSeasonsAreReadTogether(t *testing.T) {
	dir := t.TempDir()
	const seasons = 4
	for n := range seasons {
		write(t, dir, map[string]string{fmt.Sprintf("The Wire/Season %d/E01.mkv", n+1): "v"})
	}
	// Each season waits for the others to be read with it, and fails one read alone.
	var mu sync.Mutex
	in, all := 0, make(chan struct{})
	err := Walk(t.Context(), dir, []string{"."}, seasons, func(_ context.Context, f Folder, err error) error {
		if err != nil || len(f.Files) == 0 {
			return err
		}
		mu.Lock()
		if in++; in == seasons {
			close(all)
		}
		mu.Unlock()
		select {
		case <-all:
			return nil
		case <-time.After(5 * time.Second):
			return fmt.Errorf("%s was read alone", f.Path)
		}
	})
	if err != nil {
		t.Error(err)
	}
}

func TestAFolderIsReadBeforeTheFoldersInIt(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, map[string]string{
		"The Wire/Season 1/E01.mkv": "v", "The Wire/Season 2/E01.mkv": "v", "Heat (1995)/Heat.mkv": "v",
	})
	var mu sync.Mutex
	seen := map[string]bool{}
	err := Walk(t.Context(), dir, []string{"."}, 4, func(_ context.Context, f Folder, err error) error {
		mu.Lock()
		defer mu.Unlock()
		if parent := filepath.Dir(f.Path); f.Path != "." && !seen[parent] {
			return fmt.Errorf("%s read before %s", f.Path, parent)
		}
		seen[f.Path] = true
		return err
	})
	if err != nil {
		t.Error(err)
	}
}

func TestAWalkEndsAtTheFirstErrorItIsGiven(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, map[string]string{"A/1/x.mkv": "v", "B/2/y.mkv": "v", "C/3/z.mkv": "v"})
	stop := errors.New("stop")
	err := Walk(t.Context(), dir, []string{"."}, 2, func(_ context.Context, f Folder, _ error) error {
		if f.Path == "B" {
			return stop
		}
		return nil
	})
	if !errors.Is(err, stop) {
		t.Errorf("walk answered %v, want the error it was given", err)
	}
}
