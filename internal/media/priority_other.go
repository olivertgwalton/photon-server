//go:build !(linux || darwin)

package media

// lower leaves a process as it is: there is no setpriority here.
func lower(int) error { return nil }
