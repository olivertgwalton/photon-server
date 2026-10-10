//go:build !windows

package library

import "os"

func openShared(name string) (*os.File, error) { return os.Open(name) }
