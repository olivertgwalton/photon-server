package library

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"hash"
	"io/fs"
	"iter"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/olivertgwalton/photon-server/internal/naming"
)

type File struct {
	// Name is relative to the folder: a subtitle in its Subs folder is "Subs/English.srt", a tune
	// in its theme-music folder "theme-music/Main Title.mp3".
	Name    string
	Size    int64
	ModTime time.Time
}

// Folder is one directory of a library: its own files and the names of its subfolders.
type Folder struct {
	Path    string // relative to the library root; "." is the root
	Files   []File
	Folders []string
	// Skipped are the entries left out, each with why: a link to nothing, a link back to a folder
	// above it, or something that is neither a file nor a folder.
	Skipped []Skip
	// Fingerprint changes when an entry is added, removed, renamed, resized or rewritten.
	Fingerprint [sha256.Size]byte
}

type Skip struct {
	Name string // relative to the folder
	Err  error
}

var (
	errLoop      = errors.New("links back to a folder above it")
	errNotAFile  = errors.New("neither a file nor a folder")
	errNotInside = errors.New("not a path inside a library")
)

// Walk yields dir and every folder under it, dir being a path inside the library at root ("." for
// all of it), parents before children. Housekeeping names are skipped, and so is what a .ignore
// file hides, as Jellyfin's: an empty one its whole folder, else what its gitignore patterns match
// below it, until a deeper .ignore takes over. A Subs or Subtitles folder, and a theme-music folder,
// is not a folder of its own: its subtitles or sound files are listed as its parent's, so adding
// one changes the parent's fingerprint.
// Links are followed wherever they lead, to files and folders, as Jellyfin and Plex follow them:
// a library of links into a remote mount is a library like any other. A root that cannot be read
// is yielded as "." with its error.
func Walk(root, dir string) iter.Seq2[Folder, error] {
	return func(yield func(Folder, error) bool) {
		info, err := os.Stat(root)
		if err != nil {
			yield(Folder{Path: "."}, err)
			return
		}
		above, ign := []fs.FileInfo{info}, ignoreFile{}
		// The folders above dir are not read, but what they say of it holds: a .ignore in one,
		// and a link back to one.
		at := "."
		for name := range strings.SplitSeq(dir, "/") {
			if name == "." {
				break
			}
			if content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(at), ".ignore")); err == nil {
				var ok bool
				if ign, ok = parseIgnore(at, string(content)); !ok {
					return
				}
			}
			at = path.Join(at, name)
			info, err := os.Stat(filepath.Join(root, filepath.FromSlash(at)))
			if err != nil {
				yield(Folder{Path: dir}, err)
				return
			}
			if naming.Ignored(name) || ign.ignores(at, info.IsDir()) {
				return
			}
			above = append(above, info)
		}
		walk(root, dir, above, ign, yield)
	}
}

// walk reads dir, above being the folders it is in, itself last, so a link back to one of them is
// left out rather than walked forever.
func walk(root, dir string, above []fs.FileInfo, ign ignoreFile, yield func(Folder, error) bool) bool {
	full := filepath.Join(root, filepath.FromSlash(dir))
	entries, err := os.ReadDir(full)
	if err != nil {
		return yield(Folder{Path: dir}, err)
	}
	if content, err := os.ReadFile(filepath.Join(full, ".ignore")); err == nil {
		var ok bool
		if ign, ok = parseIgnore(dir, string(content)); !ok {
			return true
		}
	}
	folder := Folder{Path: dir}
	var below []fs.FileInfo
	h := sha256.New()
	for _, e := range entries {
		name := e.Name()
		if naming.Ignored(name) {
			continue
		}
		info, err := os.Stat(filepath.Join(full, name))
		rel := path.Join(dir, name)
		if ign.ignores(rel, err == nil && info.IsDir()) {
			continue
		}
		holds, foldedDir := naming.FoldedFolder(name)
		switch {
		case err != nil:
			folder.Skipped = append(folder.Skipped, Skip{name, err})
		case info.IsDir() && foldedDir:
			inside, err := os.ReadDir(filepath.Join(full, name))
			if err != nil {
				folder.Skipped = append(folder.Skipped, Skip{name, err})
				continue
			}
			for _, f := range inside {
				if holds(f.Name()) && !ign.ignores(path.Join(rel, f.Name()), false) {
					folder.add(full, h, path.Join(name, f.Name()))
				}
			}
		case info.IsDir() && slices.ContainsFunc(above, func(a fs.FileInfo) bool { return os.SameFile(a, info) }):
			folder.Skipped = append(folder.Skipped, Skip{name, errLoop})
		case info.IsDir():
			folder.Folders = append(folder.Folders, name)
			below = append(below, info)
			_, _ = fmt.Fprintf(h, "d\x00%s\n", name)
		default:
			folder.list(h, name, info)
		}
	}
	h.Sum(folder.Fingerprint[:0])
	if !yield(folder, nil) {
		return false
	}
	for i, sub := range folder.Folders {
		if !walk(root, path.Join(dir, sub), append(slices.Clip(above), below[i]), ign, yield) {
			return false
		}
	}
	return true
}

// add lists a file, name being its path relative to the folder, whose full path is full.
func (f *Folder) add(full string, h hash.Hash, name string) {
	info, err := os.Stat(filepath.Join(full, filepath.FromSlash(name)))
	if err != nil {
		f.Skipped = append(f.Skipped, Skip{name, err})
		return
	}
	f.list(h, name, info)
}

func (f *Folder) list(h hash.Hash, name string, info fs.FileInfo) {
	if !info.Mode().IsRegular() {
		f.Skipped = append(f.Skipped, Skip{name, errNotAFile})
		return
	}
	f.Files = append(f.Files, File{Name: name, Size: info.Size(), ModTime: info.ModTime()})
	_, _ = fmt.Fprintf(h, "f\x00%s\x00%d\x00%d\n", name, info.Size(), info.ModTime().UnixNano())
}

// Remove deletes a file of the library at root, as Open opens one, then each folder it leaves
// empty, up to but not root, so a film deleted from its own folder leaves no empty folder behind.
// A folder still holding anything (its poster, its NFO, another film) stays.
func Remove(root, rel string) error {
	local := filepath.FromSlash(rel)
	if !filepath.IsLocal(local) {
		return fmt.Errorf("%q: %w", rel, errNotInside)
	}
	if err := os.Remove(filepath.Join(root, local)); err != nil {
		return err
	}
	// A folder not empty is not removed, and ends the climb.
	for dir := filepath.Dir(local); dir != "." && os.Remove(filepath.Join(root, dir)) == nil; dir = filepath.Dir(dir) {
	}
	return nil
}

// Open opens a file of the library at root, rel being its path inside it as the scanner recorded
// it. A link is followed wherever it leads, as the walk followed it; what keeps a caller to the
// libraries is that every path opened is one the scanner recorded, and that none climbs out of
// root by its own name.
func Open(root, rel string) (*os.File, error) {
	if !filepath.IsLocal(filepath.FromSlash(rel)) {
		return nil, fmt.Errorf("%q: %w", rel, errNotInside)
	}
	return os.Open(filepath.Join(root, filepath.FromSlash(rel)))
}
