// Package watch scans the folders of a library again when their files change.
package watch

import (
	"context"
	"log/slog"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"uuid"

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

// Watcher watches every folder of each library set to realtime monitoring. Neither inotify nor
// FSEvents sees what another machine changes on a network share, so the scan schedule stays the
// backstop.
type Watcher struct {
	libs  libraries
	log   *slog.Logger
	roots map[uuid.UUID]string
}

func New(libs libraries, log *slog.Logger) *Watcher {
	return &Watcher{libs: libs, log: log, roots: map[uuid.UUID]string{}}
}

func (w *Watcher) Run(ctx context.Context) error {
	n, err := newNotifier(ctx, w.log)
	if err != nil {
		return err
	}
	defer n.close()
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
		case p := <-n.changed:
			lib, ok := w.owner(p)
			if !ok || ignored(w.roots[lib], p) {
				continue
			}
			if dir, ok := library.Changed(w.roots[lib], p); ok {
				if changed[lib] == nil {
					changed[lib] = map[string]bool{}
				}
				changed[lib][dir] = true
			}
		case <-n.lost:
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
func (w *Watcher) sync(ctx context.Context, n *notifier) {
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
	if maps.Equal(want, w.roots) {
		return
	}
	w.roots = want
	n.watch(ctx, slices.Compact(slices.Sorted(maps.Values(want))))
}

// ignored reports whether p, under root, is in a folder or is a file never part of a library.
func ignored(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	if err != nil || rel == "." {
		return err != nil
	}
	return slices.ContainsFunc(strings.Split(rel, string(filepath.Separator)), naming.Ignored)
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

// within reports whether p is root or under it, a root that ends in a separator, as / and D:\ do,
// among them.
func within(p, root string) bool {
	rel, err := filepath.Rel(root, p)
	return err == nil && (rel == "." || filepath.IsLocal(rel))
}
