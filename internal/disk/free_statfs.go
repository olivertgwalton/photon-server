//go:build linux || darwin

// Package disk tells the space left on a filesystem.
package disk

import "syscall"

// Free is the space left to the server under path.
func Free(path string) (uint64, bool) {
	var st syscall.Statfs_t
	if syscall.Statfs(path, &st) != nil {
		return 0, false
	}
	return st.Bavail * uint64(st.Bsize), true
}
