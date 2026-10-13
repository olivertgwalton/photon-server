// Package testtool stands in for the programs photon runs, such as ffmpeg and pg_dump, in tests.
package testtool

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Script writes a shell script named name into dir that runs body, and answers its path. The test
// is skipped where there is no POSIX shell to run it.
func Script(tb testing.TB, dir, name, body string) string {
	tb.Helper()
	if runtime.GOOS == "windows" {
		tb.Skip("needs a POSIX shell to run a fake tool")
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil { //nolint:gosec // a tool must be executable
		tb.Fatal(err)
	}
	return path
}
