package library

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// openShared opens a file to read as os.Open does, but letting others delete and rename it while
// it is open, as a file opened on Linux or macOS may be: os.Open does not, so a title played could
// be neither deleted nor upgraded by Sonarr or Radarr until its playback ended.
func openShared(name string) (*os.File, error) {
	p, err := syscall.UTF16PtrFromString(longPath(name))
	if err != nil {
		return nil, &fs.PathError{Op: "open", Path: name, Err: err}
	}
	h, err := syscall.CreateFile(p, syscall.GENERIC_READ,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,
		nil, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, &fs.PathError{Op: "open", Path: name, Err: err}
	}
	return os.NewFile(uintptr(h), name), nil
}

// longPath lets an absolute path past MAX_PATH be opened, as os.Open's own handling does.
func longPath(name string) string {
	const maxPath = 248
	if len(name) < maxPath || !filepath.IsAbs(name) || strings.HasPrefix(name, `\\?\`) {
		return name
	}
	name = filepath.Clean(name)
	if strings.HasPrefix(name, `\\`) {
		return `\\?\UNC\` + name[2:]
	}
	return `\\?\` + name
}
