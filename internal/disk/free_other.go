//go:build !(linux || darwin || windows)

package disk

// Free cannot be told without cgo or golang.org/x/sys here.
func Free(string) (uint64, bool) { return 0, false }
