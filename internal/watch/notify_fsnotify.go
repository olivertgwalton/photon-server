package watch

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"syscall"

	"github.com/fsnotify/fsnotify"

	"github.com/olivertgwalton/photon-server/internal/naming"
)

// notifier tells of changes under its roots, watching each folder of them.
type notifier struct {
	w       *fsnotify.Watcher
	log     *slog.Logger
	roots   []string
	changed chan string
	lost    chan struct{}
	// stop is closed once nothing reads changed or lost, and done once run has returned.
	stop, done chan struct{}
}

func newNotifier(ctx context.Context, log *slog.Logger) (*notifier, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	n := &notifier{
		w: w, log: log, changed: make(chan string), lost: make(chan struct{}),
		stop: make(chan struct{}), done: make(chan struct{}),
	}
	go n.run(ctx)
	return n, nil
}

func (n *notifier) run(ctx context.Context) {
	defer close(n.done)
	for {
		select {
		case ev, ok := <-n.w.Events:
			if !ok {
				return
			}
			if ev.Op == fsnotify.Chmod {
				continue
			}
			if ev.Has(fsnotify.Create) {
				if info, err := os.Stat(ev.Name); err == nil && info.IsDir() {
					n.add(ctx, ev.Name)
				}
			}
			select {
			case n.changed <- ev.Name:
			case <-n.stop:
				return
			}
		case err, ok := <-n.w.Errors:
			if !ok {
				return
			}
			if !errors.Is(err, fsnotify.ErrEventOverflow) {
				n.log.WarnContext(ctx, "watching libraries", slog.Any("err", err))
				continue
			}
			select {
			case n.lost <- struct{}{}:
			case <-n.stop:
				return
			}
		}
	}
}

// watch watches roots, and stops watching the folders of any root it watched before and not now.
func (n *notifier) watch(ctx context.Context, roots []string) {
	for _, root := range n.roots {
		if slices.Contains(roots, root) {
			continue
		}
		for _, p := range n.w.WatchList() {
			if within(p, root) {
				// One gone since is no longer watched anyway.
				if err := n.w.Remove(p); err != nil && !errors.Is(err, fsnotify.ErrNonExistentWatch) {
					n.log.WarnContext(ctx, "folder still watched", slog.String("folder", p), slog.Any("err", err))
				}
			}
		}
	}
	for _, root := range roots {
		if !slices.Contains(n.roots, root) {
			n.add(ctx, root)
		}
	}
	n.roots = roots
}

// add watches a folder and every folder under it.
func (n *notifier) add(ctx context.Context, dir string) {
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
		return n.w.Add(p)
	})
	switch {
	case errors.Is(err, syscall.ENOSPC):
		n.log.WarnContext(ctx, "out of inotify watches: the rest of this library is scanned on schedule only; "+
			"raise fs.inotify.max_user_watches", slog.String("folder", dir))
	case err != nil:
		n.log.WarnContext(ctx, "folder not watched", slog.String("folder", dir), slog.Any("err", err))
	}
}

func (n *notifier) close() {
	close(n.stop)
	_ = n.w.Close()
	<-n.done
}
