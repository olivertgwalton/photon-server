package library

import (
	"path/filepath"
	"testing"
)

func TestAChangeIsReadInTheNearestFolderThatIsThere(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, map[string]string{
		"Heat (1995)/Heat (1995).mkv": "v",
		"Heat (1995)/Subs/German.srt": "s",
	})
	for _, c := range []struct{ path, want string }{
		{"Heat (1995)", "Heat (1995)"},
		{"Heat (1995)/Heat (1995).mkv", "Heat (1995)"},
		{"Heat (1995)/Subs/German.srt", "Heat (1995)"},
		{"Heat (1995)/Featurettes/Gone.mkv", "Heat (1995)"},
		{"Alien (1979)/Alien (1979).mkv", "."},
		{"Alien.mkv", "."},
		{"", "."},
	} {
		if got, ok := Changed(dir, filepath.Join(dir, c.path)); !ok || got != c.want {
			t.Errorf("a change at %q is read in %q, %v; want %q", c.path, got, ok, c.want)
		}
	}
	if _, ok := Changed(dir, filepath.Join(dir, "..", "elsewhere")); ok {
		t.Error("a path outside the library was taken as in it")
	}
}
