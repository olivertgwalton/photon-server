//go:build !windows

package main

// asService answers that the server was not started by a service manager, which only Windows has
// in place of signals.
func asService([]string) (bool, int) { return false, 0 }
