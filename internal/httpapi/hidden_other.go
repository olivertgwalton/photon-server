//go:build !windows

package httpapi

import (
	"io/fs"
	"strings"
)

func hiddenFolder(e fs.DirEntry) bool { return strings.HasPrefix(e.Name(), ".") }
