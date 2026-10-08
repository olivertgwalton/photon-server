// Package watch scans the folders of a library again when their files change.
package watch

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"
	"uuid"

	"github.com/fsnotify/fsnotify"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/library"
	"github.com/olivertgwalton/photon-server/internal/naming"
)

const (
	// Settle is how long a library has to be quiet before its changed folders are scanned: Jellyfin's
	// LibraryMonitorDelay, long enough for a file being copied in to finish.
	Settle = time.Minute
	// resync is how often the list of libraries to watch is read again.
	resync = time.Minute
	// askEvery is how often the folders changed since are asked to be scanned: copying a file in
	// changes it on every write, which is too often to ask the database each time.
	askEvery = time.Second
)

type libraries interface {
	Libraries(ctx context.Context) ([]domain.Library, error)
	ScanFolders(ctx context.Context, id uuid.UUID, folders []string, delay time.Duration) error
}

// Watcher watches every folder of each library set to realtime monitoring. inotify watches one
// folder at a time and sees nothing on a network share, so the scan schedule stays the backstop.
type Watcher struct {
	libs  libraries
	log   *slog.Logger
	roots map[uuid.UUID]string
}

func New(libs libraries, log *slog.Logger) *Watcher {
	return &Watcher{libs: libs, log: log, roots: map[uuid.UUID]string{}}
}

func (w *Watcher) Run(ctx context.Context) error {
	n, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer n.Close()
	w.sync(ctx, n)
	tick := time.NewTicker(resync)
	defer tick.Stop()
	ask := time.NewTicker(askEvery)
	defer ask.Stop()
	changed := map[uuid.UUID]map[string]bool{}
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
			w.sync(ctx, n)
		case <-ask.C:
			for lib, folders := range changed {
				w.scan(ctx, lib, slices.Collect(maps.Keys(folders)))
			}
			clear(changed)
		case ev := <-n.Events:
			lib, ok := w.owner(ev.Name)
			if !ok || ev.Op == fsnotify.Chmod {
				continue
			}
			if ev.Has(fsnotify.Create) {
				if info, err := os.Stat(ev.Name); err == nil && info.IsDir() {
					w.add(ctx, n, lib, ev.Name)
				}
			}
			if dir, ok := library.Changed(w.roots[lib], ev.Name); ok {
				if changed[lib] == nil {
					changed[lib] = map[string]bool{}
				}
				changed[lib][dir] = true
			}
		case err := <-n.Errors:
			if !errors.Is(err, fsnotify.ErrEventOverflow) {
				w.log.WarnContext(ctx, "watching libraries", slog.Any("err", err))
				continue
			}
			// Events were lost: any folder of a watched library may have changed.
			for lib := range w.roots {
				changed[lib] = map[string]bool{".": true}
			}
		}
	}
}

// scan asks for folders of a library to be scanned once it has been quiet for Settle: each ask
// moves the scan later, so it runs after the last change.
func (w *Watcher) scan(ctx context.Context, lib uuid.UUID, folders []string) {
	if err := w.libs.ScanFolders(ctx, lib, folders, Settle); err != nil && ctx.Err() == nil {
		w.log.WarnContext(ctx, "library scan not queued", slog.Any("err", err))
	}
}

// sync starts watching libraries newly set to realtime and stops watching the rest.
func (w *Watcher) sync(ctx context.Context, n *fsnotify.Watcher) {
	libs, err := w.libs.Libraries(ctx)
	if err != nil {
		if ctx.Err() == nil {
			w.log.WarnContext(ctx, "libraries not read for watching", slog.Any("err", err))
		}
		return
	}
	want := map[uuid.UUID]string{}
	for _, l := range libs {
		if l.Monitor == domain.MonitorRealtime {
			want[l.ID] = filepath.Clean(l.Root)
		}
	}
	for id, root := range w.roots {
		if want[id] != root {
			for _, p := range n.WatchList() {
				if within(p, root) {
					// One gone since is no longer watched anyway.
					if err := n.Remove(p); err != nil && !errors.Is(err, fsnotify.ErrNonExistentWatch) {
						w.log.WarnContext(ctx, "folder still watched", slog.String("folder", p), slog.Any("err", err))
					}
				}
			}
			delete(w.roots, id)
		}
	}
	for id, root := range want {
		if _, ok := w.roots[id]; !ok {
			w.roots[id] = root
			w.add(ctx, n, id, root)
		}
	}
}

// add watches a folder and every folder under it.
func (w *Watcher) add(ctx context.Context, n *fsnotify.Watcher, lib uuid.UUID, dir string) {
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			// A folder that cannot be read is left to the scan schedule.
			return fs.SkipDir
		}
		if !d.IsDir() {
			return nil
		}
		if p != dir && naming.Ignored(d.Name()) {
			return filepath.SkipDir
		}
		return n.Add(p)
	})
	switch {
	case errors.Is(err, syscall.ENOSPC):
		w.log.WarnContext(ctx, "out of inotify watches: the rest of this library is scanned on schedule only; "+
			"raise fs.inotify.max_user_watches", slog.String("library", lib.String()), slog.String("folder", dir))
	case err != nil:
		w.log.WarnContext(ctx, "folder not watched", slog.String("folder", dir), slog.Any("err", err))
	}
}

// owner is the library a path is in: the one whose root is longest of those it is under.
func (w *Watcher) owner(p string) (uuid.UUID, bool) {
	var best uuid.UUID
	longest := -1
	for id, root := range w.roots {
		if within(p, root) && len(root) > longest {
			best, longest = id, len(root)
		}
	}
	return best, longest >= 0
}

func within(p, root string) bool {
	return p == root || strings.HasPrefix(p, root+string(filepath.Separator))
}
