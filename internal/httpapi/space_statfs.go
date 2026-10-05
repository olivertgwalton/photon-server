//go:build linux || darwin

package httpapi

import "syscall"

// freeBytes is the space left to the server under path.
func freeBytes(path string) (uint64, bool) {
	var st syscall.Statfs_t
	if syscall.Statfs(path, &st) != nil {
		return 0, false
	}
	return st.Bavail * uint64(st.Bsize), true
}
