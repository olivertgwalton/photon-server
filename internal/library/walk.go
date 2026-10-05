package library

import (
	"crypto/sha256"
	"fmt"
	"hash"
	"io/fs"
	"iter"
	"os"
	"path"
	"time"

	"github.com/olivertgwalton/photon-server/internal/naming"
)

type File struct {
	// Name is relative to the folder: a subtitle in its Subs folder is "Subs/English.srt".
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
// and so is any folder holding a .ignore file. A Subs or Subtitles folder is not a folder of its
// own: its subtitles are listed as its parent's, so adding one changes the parent's fingerprint. A symlink is followed only to a file inside root:
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
		switch {
		case e.IsDir() && naming.SubtitleFolder(name):
			subs, err := fs.ReadDir(root.FS(), rel)
			if err != nil {
				continue
			}
			for _, sub := range subs {
				if naming.IsSubtitle(sub.Name()) {
					folder.add(root, h, path.Join(name, sub.Name()))
				}
			}
		case e.IsDir():
			folder.Folders = append(folder.Folders, name)
			_, _ = fmt.Fprintf(h, "d\x00%s\n", name)
		default:
			folder.add(root, h, name)
		}
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

// add lists a regular file, name being its path relative to the folder.
func (f *Folder) add(root *os.Root, h hash.Hash, name string) {
	info, err := root.Stat(path.Join(f.Path, name))
	if err != nil || !info.Mode().IsRegular() {
		return
	}
	f.Files = append(f.Files, File{Name: name, Size: info.Size(), ModTime: info.ModTime()})
	_, _ = fmt.Fprintf(h, "f\x00%s\x00%d\x00%d\n", name, info.Size(), info.ModTime().UnixNano())
}
