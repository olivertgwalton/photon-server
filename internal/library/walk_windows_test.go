package library

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// A junction back up the library is left out as a loop, as a link is, rather than walked until
// the path is too long.
func TestWalkLeavesOutAJunctionBackUp(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, map[string]string{"Heat (1995)/Heat (1995).mkv": "v"})
	junction := filepath.Join(dir, "Heat (1995)", "back up")
	if out, err := exec.CommandContext(t.Context(), "cmd", "/c", "mklink", "/J", junction, dir).CombinedOutput(); err != nil {
		t.Fatalf("mklink: %v: %s", err, out)
	}
	t.Cleanup(func() { _ = os.Remove(junction) })

	skipped := map[string]error{}
	for _, s := range walkAll(t, dir)["Heat (1995)"].Skipped {
		skipped[s.Name] = s.Err
	}
	if err := skipped["back up"]; !errors.Is(err, errLoop) {
		t.Errorf("a junction to the library: %v, want it left out as a loop", err)
	}
}
