//go:build linux || darwin

package media

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

func niceness(t *testing.T, pid int) int {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), "ps", "-o", "nice=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		t.Fatal(err)
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// A background tool runs at niceness 10, as Jellyfin's BelowNormal, and a foreground one, as a
// playback's remux, at the server's own.
func TestABackgroundToolRunsAtALowerPriority(t *testing.T) {
	server := niceness(t, os.Getpid())
	for priority, want := range map[Priority]int{Foreground: server, Background: max(backgroundNice, server)} {
		c := NewCommand(t.Context(), priority, nil, "sleep", "5")
		if err := c.Start(); err != nil {
			t.Fatal(err)
		}
		got := niceness(t, c.Process.Pid)
		if err := c.Process.Kill(); err != nil {
			t.Fatal(err)
		}
		var killed *exec.ExitError
		if err := c.Wait(); err != nil && !errors.As(err, &killed) {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("%s: niceness %d, want %d", priority, got, want)
		}
	}
}
