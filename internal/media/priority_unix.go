//go:build linux || darwin

package media

import (
	"errors"
	"syscall"
)

// lower gives a process backgroundNice. A process already gone (ESRCH) is not lowered, nor is one
// whose server already runs lower than backgroundNice (EPERM, EACCES: only root raises a priority),
// and neither is an error.
func lower(pid int) error {
	err := syscall.Setpriority(syscall.PRIO_PROCESS, pid, backgroundNice)
	if errors.Is(err, syscall.ESRCH) || errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EACCES) {
		return nil
	}
	return err
}
