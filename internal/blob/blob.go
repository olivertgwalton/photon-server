// Package blob keeps objects by key: each written whole or not at all, and read back from anywhere
// in it.
package blob

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"iter"
	"maps"
	"net/http"
	"os"
	"path"
	"strings"
	"time"
	"uuid"
)

// Object is an object's bytes and when they were written.
type Object struct {
	io.ReadSeekCloser
	ModTime time.Time
}

// OfFile is f as an object, closing f if it cannot say when it was written.
func OfFile(f *os.File) (Object, error) {
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return Object{}, err
	}
	return Object{ReadSeekCloser: f, ModTime: info.ModTime()}, nil
}

// Entry is an object listed.
type Entry struct {
	Key     string
	ModTime time.Time
}

const (
	// parts holds objects being written, outside every key's reach.
	parts = ".parts"
	// partLife is how long an object may be being written before it is taken for one abandoned.
	partLife = time.Hour
)

// Dir keeps objects as files in a folder, the slashes of a key its folders.
type Dir struct {
	root *os.Root
}

// OpenDir opens the folder dir as a Dir, making it if need be, and removes the objects it was
// left writing long ago.
func OpenDir(dir string) (*Dir, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	d := &Dir{root: root}
	if err := d.removeAbandoned(); err != nil {
		_ = root.Close()
		return nil, err
	}
	return d, nil
}

func (d *Dir) removeAbandoned() error {
	if err := d.root.MkdirAll(parts, 0o750); err != nil {
		return err
	}
	entries, err := fs.ReadDir(d.root.FS(), parts)
	if err != nil {
		return err
	}
	for _, e := range entries {
		// Another process sharing the folder may be writing it still.
		if info, err := e.Info(); err == nil && time.Since(info.ModTime()) > partLife {
			if err := d.root.Remove(path.Join(parts, e.Name())); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return err
			}
		}
	}
	return nil
}

func (d *Dir) Close() error { return d.root.Close() }

// Open answers the object under key, or fs.ErrNotExist.
func (d *Dir) Open(_ context.Context, key string) (Object, error) {
	f, err := d.root.Open(key)
	if err != nil {
		return Object{}, err
	}
	info, err := f.Stat()
	if err == nil && info.IsDir() {
		// A folder is only the start of other keys.
		err = &fs.PathError{Op: "open", Path: key, Err: fs.ErrNotExist}
	}
	if err != nil {
		_ = f.Close()
		return Object{}, err
	}
	return Object{ReadSeekCloser: f, ModTime: info.ModTime()}, nil
}

// Exists answers whether there is an object under key.
func (d *Dir) Exists(_ context.Context, key string) (bool, error) {
	info, err := d.root.Stat(key)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return err == nil && !info.IsDir(), err
}

// Put keeps what r reads under key, in place of any object there. Should r fail, nothing is kept.
func (d *Dir) Put(_ context.Context, key string, r io.Reader) error {
	part := path.Join(parts, uuid.NewV7().String())
	f, err := d.root.Create(part)
	if err != nil {
		return err
	}
	_, err = io.Copy(f, r)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = d.root.MkdirAll(path.Dir(key), 0o750)
	}
	if err == nil {
		err = d.root.Rename(part, key)
	}
	if err != nil {
		_ = d.root.Remove(part)
	}
	return err
}

// Delete removes the object under key, if there is one.
func (d *Dir) Delete(_ context.Context, key string) error {
	if err := d.root.Remove(key); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	// A folder goes with its last object, as there are no folders but in keys; one not empty stays.
	for dir := path.Dir(key); dir != "." && d.root.Remove(dir) == nil; dir = path.Dir(dir) {
	}
	return nil
}

// List answers the objects whose keys start with prefix, in the order of their keys.
func (d *Dir) List(_ context.Context, prefix string) iter.Seq2[Entry, error] {
	return func(yield func(Entry, error) bool) {
		// Only the folder the prefix ends in is walked, not every object kept.
		start := "."
		if i := strings.LastIndexByte(prefix, '/'); i > 0 {
			start = prefix[:i]
		}
		err := fs.WalkDir(d.root.FS(), start, func(name string, e fs.DirEntry, err error) error {
			if errors.Is(err, fs.ErrNotExist) && name == start {
				return fs.SkipAll
			}
			if err != nil {
				return err
			}
			if e.IsDir() {
				if name == parts {
					return fs.SkipDir
				}
				return nil
			}
			if !strings.HasPrefix(name, prefix) {
				return nil
			}
			info, err := e.Info()
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			if err != nil {
				return err
			}
			if !yield(Entry{Key: name, ModTime: info.ModTime()}, nil) {
				return fs.SkipAll
			}
			return nil
		})
		if err != nil {
			yield(Entry{}, err)
		}
	}
}

// Serve answers r with o, which it closes, in byte ranges and with header; or, for an object a
// client may read where it is kept, sends it there, keeping the redirect until the link must not
// be followed. name gives the type where header does not.
func Serve(w http.ResponseWriter, r *http.Request, o Object, name string, header http.Header) error {
	defer o.Close()
	if l, ok := o.ReadSeekCloser.(*linked); ok {
		link, until, err := l.link(r.Context(), header)
		if err != nil {
			return err
		}
		w.Header().Set("Cache-Control", fmt.Sprintf("private, max-age=%d", int(time.Until(until).Seconds())))
		http.Redirect(w, r, link, http.StatusFound)
		return nil
	}
	maps.Copy(w.Header(), header)
	// The reader itself, not o: a file is sent by sendfile only as an *os.File.
	http.ServeContent(w, r, name, o.ModTime, o.ReadSeekCloser)
	return nil
}
