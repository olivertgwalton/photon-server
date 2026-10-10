//go:build !windows

package media

import (
	"os"
	"syscall"
)

// stop asks a tool to stop with SIGTERM, so ffmpeg can finish what it is writing.
func stop(p *os.Process) error { return p.Signal(syscall.SIGTERM) }
