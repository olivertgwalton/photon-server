//go:build !(linux || darwin)

package httpapi

// freeBytes cannot be told without cgo or golang.org/x/sys here.
func freeBytes(string) (uint64, bool) { return 0, false }
