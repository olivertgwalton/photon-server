package library

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"iter"
	"os"
	"path"
	"time"

	"github.com/olivertgwalton/photon-server/internal/naming"
)

type File struct {
	Name    string
	Size    int64
	ModTime time.Time
}

// Folder is one directory of a library: its own files and the names of its subfolders.
type Folder struct {
	Path    string // relative to the library root; "." is the root
	Files   []File
	Folders []string
	// Fingerprint changes when an entry is added, removed, renamed, resized or rewritten.
	Fingerprint [sha256.Size]byte
}

// Walk yields every folder under root, parents before children. Housekeeping names are skipped,
// and so is any folder holding a .ignore file. A symlink is followed only to a file inside root:
// os.Root refuses one that leaves it, and symlinked folders are not followed, so a loop cannot
// form.
func Walk(root *os.Root) iter.Seq2[Folder, error] {
	return func(yield func(Folder, error) bool) {
		walk(root, ".", yield)
	}
}

func walk(root *os.Root, dir string, yield func(Folder, error) bool) bool {
	entries, err := fs.ReadDir(root.FS(), dir)
	if err != nil {
		return yield(Folder{Path: dir}, err)
	}
	folder := Folder{Path: dir}
	h := sha256.New()
	for _, e := range entries {
		if e.Name() == ".ignore" {
			return true
		}
	}
	for _, e := range entries {
		name := e.Name()
		if naming.Ignored(name) {
			continue
		}
		rel := path.Join(dir, name)
		if e.IsDir() {
			folder.Folders = append(folder.Folders, name)
			_, _ = fmt.Fprintf(h, "d\x00%s\n", name)
			continue
		}
		info, err := root.Stat(rel)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		folder.Files = append(folder.Files, File{Name: name, Size: info.Size(), ModTime: info.ModTime()})
		_, _ = fmt.Fprintf(h, "f\x00%s\x00%d\x00%d\n", name, info.Size(), info.ModTime().UnixNano())
	}
	h.Sum(folder.Fingerprint[:0])
	if !yield(folder, nil) {
		return false
	}
	for _, sub := range folder.Folders {
		if !walk(root, path.Join(dir, sub), yield) {
			return false
		}
	}
	return true
}
