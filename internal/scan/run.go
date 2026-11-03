package scan

import (
	"context"
	"crypto/sha256"
	"path"
	"strings"
	"sync"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/library"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// run is one scan of a library, shared by the folders it reads at once.
type run struct {
	lib          domain.Library
	fingerprints map[string][]byte
	reads        chan struct{}
	progress     func(domain.ScanProgress)
	changed      func(store.Changed)
	// saving writes one folder at a time: two folders holding the same bytes, or the same show,
	// would otherwise each add them.
	saving sync.Mutex
	mu     sync.Mutex
	report Report
	told   domain.ScanProgress
	// folders and present are every folder read and every video and subtitle file in them.
	folders, present []string
	// shows says of each folder of a mixed library known to the run whether it is a show's own.
	shows map[string]bool
}

// seriesOf is the folder of the series a folder of the library is in, none for a folder of films:
// a shows library's first folder down from its root, and a mixed library's first one down that
// isShow says is a show's, those below it being its seasons, as Jellyfin's are.
func (r *run) seriesOf(folder library.Folder) (string, error) {
	switch r.lib.Kind {
	case domain.LibraryMovies:
		return "", nil
	case domain.LibraryShows:
		return firstFolder(folder.Path), nil
	case domain.LibraryMixed:
	}
	r.mu.Lock()
	r.shows[folder.Path] = isShow(folder)
	r.mu.Unlock()
	at := "."
	for name := range strings.SplitSeq(folder.Path, "/") {
		if name == "." {
			break
		}
		at = path.Join(at, name)
		show, err := r.isShow(at)
		if err != nil || show {
			return at, err
		}
	}
	return "", nil
}

// isShow is whether a folder of a mixed library is a show's own. Folders come before those under
// them, so one the run read is known; one above the folders the scan asked for is read now.
func (r *run) isShow(rel string) (bool, error) {
	r.mu.Lock()
	show, known := r.shows[rel]
	r.mu.Unlock()
	if known {
		return show, nil
	}
	f, err := library.Read(r.lib.Root, rel)
	if err != nil {
		return false, err
	}
	show = isShow(f)
	r.mu.Lock()
	r.shows[rel] = show
	r.mu.Unlock()
	return show, nil
}

// remembered is what a folder's scan is remembered by: its fingerprint and, in a mixed library,
// the series it was read as part of, so a folder a change above it makes a show's, or no longer
// one's, is read again.
func remembered(lib domain.LibraryKind, f library.Folder, series string) []byte {
	if lib != domain.LibraryMixed {
		return f.Fingerprint[:]
	}
	h := sha256.New()
	h.Write(f.Fingerprint[:])
	h.Write([]byte(series))
	return h.Sum(nil)
}

// read takes one of the run's slots for reading files, to hand back with the function it answers.
func (r *run) read(ctx context.Context) (func(), error) {
	select {
	case r.reads <- struct{}{}:
		return func() { <-r.reads }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (r *run) count(f func(*Report)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	f(&r.report)
}

// done tells that one more folder has been read.
func (r *run) done(folder string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.told.Done++
	r.told.Folder = strings.TrimPrefix(folder, ".")
	r.progress(r.told)
}
