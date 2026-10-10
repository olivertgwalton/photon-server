package httpapi

import (
	"io/fs"
	"strings"
	"syscall"
)

// hiddenFolder is a folder named as a hidden one is elsewhere, or marked hidden, as a drive's
// $RECYCLE.BIN and System Volume Information are.
func hiddenFolder(e fs.DirEntry) bool {
	if strings.HasPrefix(e.Name(), ".") {
		return true
	}
	info, err := e.Info()
	if err != nil {
		return false
	}
	attrs, ok := info.Sys().(*syscall.Win32FileAttributeData)
	return ok && attrs.FileAttributes&syscall.FILE_ATTRIBUTE_HIDDEN != 0
}
