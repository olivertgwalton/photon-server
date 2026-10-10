package library

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"hash"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

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

// Walk reads each of dirs, paths inside the library at root ("." for all of it), and every folder
// under them, calling each with every folder read or that could not be read, atOnce folders at a
// time from one queue, so a show's seasons are read together as readily as two shows. A folder's
// subfolders are read once each has returned for it: parents come before children. Walk ends at the
// first error each answers, and answers it.
// Housekeeping names are skipped, and so is what a .ignore file hides, as Jellyfin's: an empty one
// its whole folder, else what its gitignore patterns match below it, until a deeper .ignore takes
// over. A Subs or Subtitles folder, and a theme-music folder, is not a folder of its own: its
// subtitles or sound files are listed as its parent's, so adding one changes the parent's
// fingerprint.
// Links are followed wherever they lead, to files and folders, as Jellyfin and Plex follow them:
// a library of links into a remote mount is a library like any other. A root that cannot be read
// is passed to each as "." with its error.
func Walk(ctx context.Context, root string, dirs []string, atOnce int, each func(context.Context, Folder, error) error) error {
	g, ctx := errgroup.WithContext(ctx)
	w := &walker{root: root, each: each}
	w.more = sync.NewCond(&w.mu)
	stop := context.AfterFunc(ctx, func() {
		w.mu.Lock()
		defer w.mu.Unlock()
		w.more.Broadcast()
	})
	defer stop()
	for _, dir := range slices.Backward(dirs) {
		w.queue = append(w.queue, visit{dir: dir, start: true})
	}
	for range atOnce {
		g.Go(func() error { return w.work(ctx) })
	}
	return g.Wait()
}

// visit is a folder to read, and what is known of it from the folders above it: the full path of
// each, itself last, and the .ignore that holds there. A start is a folder asked for, whose
// folders above have not been read.
type visit struct {
	dir   string
	above []string
	ign   ignoreFile
	start bool
}

type walker struct {
	root string
	each func(context.Context, Folder, error) error
	mu   sync.Mutex
	more *sync.Cond
	// queue is taken from its end, so a walk goes deep before wide and keeps few folders waiting.
	queue []visit
	busy  int
}

func (w *walker) work(ctx context.Context) error {
	for {
		w.mu.Lock()
		for len(w.queue) == 0 && w.busy > 0 && ctx.Err() == nil {
			w.more.Wait()
		}
		if len(w.queue) == 0 || ctx.Err() != nil {
			w.more.Broadcast()
			w.mu.Unlock()
			return ctx.Err()
		}
		v := w.queue[len(w.queue)-1]
		w.queue = w.queue[:len(w.queue)-1]
		w.busy++
		w.mu.Unlock()

		below, err := w.visit(ctx, v)

		w.mu.Lock()
		w.busy--
		w.queue = append(w.queue, below...)
		w.more.Broadcast()
		w.mu.Unlock()
		if err != nil {
			return err
		}
	}
}

// visit reads one folder and hands it to each, answering its subfolders to read, last first.
func (w *walker) visit(ctx context.Context, v visit) ([]visit, error) {
	if v.start {
		var ok bool
		var err error
		if v, ok, err = w.climb(v.dir); !ok {
			return nil, err
		}
		if err != nil {
			return nil, w.each(ctx, Folder{Path: v.dir}, err)
		}
	}
	full := v.above[len(v.above)-1]
	entries, err := os.ReadDir(full)
	if err != nil {
		return nil, w.each(ctx, Folder{Path: v.dir}, err)
	}
	if slices.ContainsFunc(entries, func(e fs.DirEntry) bool { return e.Name() == ".ignore" }) {
		if content, err := os.ReadFile(filepath.Join(full, ".ignore")); err == nil {
			var ok bool
			if v.ign, ok = parseIgnore(v.dir, string(content)); !ok {
				return nil, nil
			}
		}
	}
	folder := read(full, v, entries)
	if err := w.each(ctx, folder, nil); err != nil {
		return nil, err
	}
	below := make([]visit, 0, len(folder.Folders))
	for _, sub := range slices.Backward(folder.Folders) {
		below = append(below, visit{
			dir: path.Join(v.dir, sub), above: append(slices.Clip(v.above), filepath.Join(full, sub)), ign: v.ign,
		})
	}
	return below, nil
}

// climb answers the visit of dir, a folder asked for: the folders above it are not read, but what
// they say of it holds, a .ignore in one, and a link back to one. It answers false for a dir one
// hides, and the error of a folder on the way that cannot be read, as "." for the root.
func (w *walker) climb(dir string) (visit, bool, error) {
	v := visit{dir: dir, above: []string{w.root}}
	if _, err := os.Stat(w.root); err != nil {
		v.dir = "."
		return v, true, err
	}
	at := "."
	for name := range strings.SplitSeq(dir, "/") {
		if name == "." {
			break
		}
		if content, err := os.ReadFile(filepath.Join(w.root, filepath.FromSlash(at), ".ignore")); err == nil {
			var ok bool
			if v.ign, ok = parseIgnore(at, string(content)); !ok {
				return v, false, nil
			}
		}
		at = path.Join(at, name)
		full := filepath.Join(w.root, filepath.FromSlash(at))
		info, err := os.Stat(full)
		if err != nil {
			return v, true, err
		}
		if naming.Ignored(name) || v.ign.ignores(at, info.IsDir()) {
			return v, false, nil
		}
		v.above = append(v.above, full)
	}
	return v, true, nil
}

// read lists the entries of the folder at full. Only a file, or a link, is looked up: a folder's
// entry already says it is one, and on a remote mount every lookup is a round trip.
func read(full string, v visit, entries []fs.DirEntry) Folder {
	folder := Folder{Path: v.dir}
	h := sha256.New()
	for _, e := range entries {
		name := e.Name()
		if naming.Ignored(name) {
			continue
		}
		var info fs.FileInfo
		var err error
		isDir, link := e.IsDir(), IsLink(e)
		if link || !isDir {
			info, err = os.Stat(filepath.Join(full, name))
			isDir = err == nil && info.IsDir()
		}
		rel := path.Join(v.dir, name)
		if v.ign.ignores(rel, isDir) {
			continue
		}
		holds, foldedDir := naming.FoldedFolder(name)
		switch {
		case err != nil:
			folder.Skipped = append(folder.Skipped, Skip{name, err})
		case isDir && foldedDir:
			inside, err := os.ReadDir(filepath.Join(full, name))
			if err != nil {
				folder.Skipped = append(folder.Skipped, Skip{name, err})
				continue
			}
			for _, f := range inside {
				if holds(f.Name()) && !v.ign.ignores(path.Join(rel, f.Name()), false) {
					folder.add(full, h, path.Join(name, f.Name()))
				}
			}
		// Only a link can lead back up: a folder cannot be hard-linked.
		case isDir && link && leadsAbove(info, v.above):
			folder.Skipped = append(folder.Skipped, Skip{name, errLoop})
		case isDir:
			folder.Folders = append(folder.Folders, name)
			h.Write(fmt.Appendf(nil, "d\x00%s\n", name))
		default:
			folder.list(h, name, info)
		}
	}
	h.Sum(folder.Fingerprint[:0])
	return folder
}

// IsLink is whether an entry leads elsewhere: a symbolic link, or a junction, which Go reports on
// Windows as irregular.
func IsLink(e fs.DirEntry) bool {
	return e.Type()&(fs.ModeSymlink|fs.ModeIrregular) != 0
}

func leadsAbove(info fs.FileInfo, above []string) bool {
	return slices.ContainsFunc(above, func(dir string) bool {
		a, err := os.Stat(dir)
		return err == nil && os.SameFile(a, info)
	})
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
	h.Write(fmt.Appendf(nil, "f\x00%s\x00%d\x00%d\n", name, info.Size(), info.ModTime().UnixNano()))
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
	return openShared(filepath.Join(root, filepath.FromSlash(rel)))
}
