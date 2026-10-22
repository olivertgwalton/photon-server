//go:build darwin

package watch

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
)

const (
	// coalesce is how long FSEvents gathers changes before it tells of them: Run gathers them for
	// askEvery itself.
	coalesce = 100 * time.Millisecond
	// pending is how many changes are held for Run to read; more at once are told as lost, as
	// FSEvents tells of those it drops itself.
	pending = 1024

	sinceNow     = ^uint64(0)
	utf8Encoding = 0x08000100

	createNoDefer    = 0x02
	createWatchRoot  = 0x04
	createFileEvents = 0x10

	eventMustScanSubDirs = 0x01
	eventHistoryDone     = 0x10
	eventRootChanged     = 0x20
	eventMount           = 0x40
	eventUnmount         = 0x80
)

// fsevents is CoreServices' FSEvents, and what it needs of CoreFoundation and libdispatch, called
// through purego so the server builds without cgo.
var fsevents struct {
	cfStringCreateWithCString func(alloc uintptr, s *byte, encoding uint32) uintptr
	cfArrayCreate             func(alloc uintptr, values *uintptr, n int, callbacks uintptr) uintptr
	cfRelease                 func(ref uintptr)
	arrayCallbacks            uintptr

	streamCreate           func(alloc, callback uintptr, ctx *streamContext, paths uintptr, since uint64, latency float64, flags uint32) uintptr
	streamSetDispatchQueue func(stream, queue uintptr)
	streamStart            func(stream uintptr) bool
	streamStop             func(stream uintptr)
	streamInvalidate       func(stream uintptr)
	streamRelease          func(stream uintptr)

	queueCreate  func(label *byte, attr uintptr) uintptr
	queueRelease func(queue uintptr)

	callback uintptr
}

var loadFSEvents = sync.OnceValue(func() error {
	libs := map[string]uintptr{}
	for _, path := range []string{
		"/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation",
		"/System/Library/Frameworks/CoreServices.framework/CoreServices",
		"/usr/lib/libSystem.B.dylib",
	} {
		h, err := purego.Dlopen(path, purego.RTLD_LAZY|purego.RTLD_GLOBAL)
		if err != nil {
			return fmt.Errorf("watch: %s: %w", path, err)
		}
		libs[filepath.Base(path)] = h
	}
	f := &fsevents
	for _, fn := range []struct {
		lib, name string
		ptr       any
	}{
		{"CoreFoundation", "CFStringCreateWithCString", &f.cfStringCreateWithCString},
		{"CoreFoundation", "CFArrayCreate", &f.cfArrayCreate},
		{"CoreFoundation", "CFRelease", &f.cfRelease},
		{"CoreServices", "FSEventStreamCreate", &f.streamCreate},
		{"CoreServices", "FSEventStreamSetDispatchQueue", &f.streamSetDispatchQueue},
		{"CoreServices", "FSEventStreamStart", &f.streamStart},
		{"CoreServices", "FSEventStreamStop", &f.streamStop},
		{"CoreServices", "FSEventStreamInvalidate", &f.streamInvalidate},
		{"CoreServices", "FSEventStreamRelease", &f.streamRelease},
		{"libSystem.B.dylib", "dispatch_queue_create", &f.queueCreate},
		{"libSystem.B.dylib", "dispatch_release", &f.queueRelease},
	} {
		sym, err := purego.Dlsym(libs[fn.lib], fn.name)
		if err != nil {
			return fmt.Errorf("watch: %s: %w", fn.name, err)
		}
		purego.RegisterFunc(fn.ptr, sym)
	}
	var err error
	if f.arrayCallbacks, err = purego.Dlsym(libs["CoreFoundation"], "kCFTypeArrayCallBacks"); err != nil {
		return fmt.Errorf("watch: kCFTypeArrayCallBacks: %w", err)
	}
	// A callback made is never freed, so every stream shares one, telling its notifier by info.
	f.callback = purego.NewCallback(streamCallback)
	return nil
})

// streamContext is an FSEventStreamContext: version 0, info, and no retain, release or
// copyDescription.
type streamContext struct {
	_    int
	info uintptr
	_    [3]uintptr
}

// notifiers are the notifiers open, by the info their stream calls back with: a number, not a Go
// pointer, as C keeps it.
var (
	notifiers sync.Map
	lastID    atomic.Uintptr
)

// notifier tells of changes under its roots by FSEvents: one stream for every root and all under
// it, where kqueue holds every file and folder watched open.
type notifier struct {
	log     *slog.Logger
	id      uintptr
	queue   uintptr
	stream  uintptr
	changed chan string
	lost    chan struct{}
	// last is the newest event told of, from which a stream made again for other roots goes on.
	last atomic.Uint64

	mu sync.Mutex
	// real maps each root as FSEvents names it, its links followed, to the root it was asked as.
	real map[string]string
}

func newNotifier(_ context.Context, log *slog.Logger) (*notifier, error) {
	if err := loadFSEvents(); err != nil {
		return nil, err
	}
	n := &notifier{log: log, id: lastID.Add(1), changed: make(chan string, pending), lost: make(chan struct{}, 1)}
	label := []byte("photon.watch\x00")
	n.queue = fsevents.queueCreate(&label[0], 0)
	notifiers.Store(n.id, n)
	return n, nil
}

// watch watches roots, in place of what it watched before.
func (n *notifier) watch(ctx context.Context, roots []string) {
	n.release()
	followed := map[string]string{}
	refs := make([]uintptr, 0, len(roots))
	for _, root := range roots {
		r, err := filepath.EvalSymlinks(root)
		if err != nil {
			r = root
		}
		followed[r] = root
		s := append([]byte(r), 0)
		refs = append(refs, fsevents.cfStringCreateWithCString(0, &s[0], utf8Encoding))
		runtime.KeepAlive(s)
	}
	n.mu.Lock()
	n.real = followed
	n.mu.Unlock()
	if len(refs) == 0 {
		return
	}
	paths := fsevents.cfArrayCreate(0, &refs[0], len(refs), fsevents.arrayCallbacks)
	for _, r := range refs {
		fsevents.cfRelease(r)
	}
	since := n.last.Load()
	if since == 0 {
		since = sinceNow
	}
	n.stream = fsevents.streamCreate(0, fsevents.callback, &streamContext{info: n.id}, paths, since, coalesce.Seconds(),
		createNoDefer|createWatchRoot|createFileEvents)
	fsevents.cfRelease(paths)
	if n.stream == 0 {
		n.log.WarnContext(ctx, "libraries not watched: FSEvents made no stream", slog.Any("roots", roots))
		return
	}
	fsevents.streamSetDispatchQueue(n.stream, n.queue)
	if !fsevents.streamStart(n.stream) {
		n.log.WarnContext(ctx, "libraries not watched: FSEvents did not start", slog.Any("roots", roots))
		n.release()
	}
}

func (n *notifier) release() {
	if n.stream == 0 {
		return
	}
	fsevents.streamStop(n.stream)
	fsevents.streamInvalidate(n.stream)
	fsevents.streamRelease(n.stream)
	n.stream = 0
}

func (n *notifier) close() {
	n.release()
	notifiers.Delete(n.id)
	fsevents.queueRelease(n.queue)
}

// asked is p under the root it was asked as.
func (n *notifier) asked(p string) string {
	n.mu.Lock()
	defer n.mu.Unlock()
	for followed, root := range n.real {
		if rel, err := filepath.Rel(followed, p); err == nil && within(p, followed) {
			return filepath.Join(root, rel)
		}
	}
	return p
}

// tell passes on a change, or that changes were lost where Run has yet to read those before: the
// stream's queue must never wait on Run, which may itself be making the stream again.
func (n *notifier) tell(p string) {
	select {
	case n.changed <- p:
	default:
		n.tellLost()
	}
}

func (n *notifier) tellLost() {
	select {
	case n.lost <- struct{}{}:
	default:
	}
}

// streamCallback is an FSEventStreamCallback, called on the stream's queue with its events' paths
// as C strings, and their flags and IDs.
func streamCallback(_, info, count uintptr, paths, flags, ids unsafe.Pointer) {
	v, _ := notifiers.Load(info)
	n, ok := v.(*notifier)
	if !ok {
		return
	}
	ps := unsafe.Slice((**byte)(paths), count)
	fs := unsafe.Slice((*uint32)(flags), count)
	is := unsafe.Slice((*uint64)(ids), count)
	for i, p := range ps {
		n.last.Store(is[i])
		switch f := fs[i]; {
		case f&eventHistoryDone != 0:
		case f&(eventMustScanSubDirs|eventRootChanged|eventMount|eventUnmount) != 0:
			// FSEvents dropped events under the path, or the root itself moved or a disk came or
			// went under it: any folder may have changed.
			n.tellLost()
		default:
			n.tell(n.asked(strings.Clone(unsafe.String(p, cstrlen(p)))))
		}
	}
}

func cstrlen(p *byte) int {
	n := 0
	for *(*byte)(unsafe.Add(unsafe.Pointer(p), n)) != 0 {
		n++
	}
	return n
}
