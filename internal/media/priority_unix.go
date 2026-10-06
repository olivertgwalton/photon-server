//go:build linux || darwin

package media

import "syscall"

func lower(pid int) error {
	return syscall.Setpriority(syscall.PRIO_PROCESS, pid, backgroundNice)
}
