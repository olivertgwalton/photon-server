package watch

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

type ask struct {
	lib     uuid.UUID
	folders []string
}

type fakeLibraries struct {
	libs    []domain.Library
	scanned chan ask
}

func (f fakeLibraries) Libraries(context.Context) ([]domain.Library, error) { return f.libs, nil }

func (f fakeLibraries) ScanFolders(_ context.Context, id uuid.UUID, folders []string, delay time.Duration) error {
	if delay == Settle {
		f.scanned <- ask{id, folders}
	}
	return nil
}

func TestAChangeInAWatchedLibraryQueuesItsScan(t *testing.T) {
	films, tv := t.TempDir(), t.TempDir()
	watched, unwatched := uuid.NewV7(), uuid.NewV7()
	f := fakeLibraries{
		libs: []domain.Library{
			{ID: watched, Root: films, Monitor: domain.MonitorRealtime},
			{ID: unwatched, Root: tv, Monitor: domain.MonitorOff},
		},
		scanned: make(chan ask, 64),
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error)
	go func() { done <- New(f, slog.New(slog.DiscardHandler)).Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	})
	// touch rewrites path until a scan of folder is queued, as the watcher may not yet watch its
	// folder.
	touch := func(path, folder string) {
		t.Helper()
		deadline := time.After(5 * time.Second)
		for n := 0; ; n++ {
			if err := os.WriteFile(path, []byte{byte(n)}, 0o644); err != nil {
				t.Fatal(err)
			}
			select {
			case a := <-f.scanned:
				if a.lib != watched || !slices.Equal(a.folders, []string{folder}) {
					t.Fatalf("queued a scan of %v in %v, want %s in the watched library", a.folders, a.lib, folder)
				}
				return
			case <-time.After(50 * time.Millisecond):
			case <-deadline:
				t.Fatalf("writing %s queued no scan", path)
			}
		}
	}
	touch(filepath.Join(films, "Alien.mkv"), ".")

	if err := os.WriteFile(filepath.Join(tv, "Show.mkv"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(films, "Heat (1995)"), 0o755); err != nil {
		t.Fatal(err)
	}
	// The new folder queues a scan of its own, and the writes before it may be told of in the ask
	// after theirs; only a scan after those is the file's.
	for quiet := time.After(2 * askEvery); ; {
		select {
		case <-f.scanned:
			quiet = time.After(2 * askEvery)
			continue
		case <-quiet:
		}
		break
	}
	// A film's folder is read alone, its subtitles' folder with it.
	touch(filepath.Join(films, "Heat (1995)", "Heat.mkv"), "Heat (1995)")
	if err := os.Mkdir(filepath.Join(films, "Heat (1995)", "Subs"), 0o755); err != nil {
		t.Fatal(err)
	}
	touch(filepath.Join(films, "Heat (1995)", "Subs", "English.srt"), "Heat (1995)")
}

// Copying a film in writes to it thousands of times; the scan is asked for a handful of times, not
// once a write.
func TestAFileBeingWrittenAsksForItsScanSeldom(t *testing.T) {
	films := t.TempDir()
	lib := uuid.NewV7()
	f := fakeLibraries{
		libs:    []domain.Library{{ID: lib, Root: films, Monitor: domain.MonitorRealtime}},
		scanned: make(chan ask, 4096),
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error)
	go func() { done <- New(f, slog.New(slog.DiscardHandler)).Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	})
	path := filepath.Join(films, "Alien.mkv")
	// Once one write is seen, the folder is watched.
	for deadline := time.After(5 * time.Second); len(f.scanned) == 0; {
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		select {
		case <-deadline:
			t.Fatal("writing queued no scan")
		case <-time.After(50 * time.Millisecond):
		}
	}
	<-f.scanned
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	for range 1000 {
		if _, err := file.Write(make([]byte, 1024)); err != nil {
			t.Fatal(err)
		}
	}
	_ = file.Close()
	<-time.After(3 * askEvery)
	if n := len(f.scanned); n == 0 || n > 3 {
		t.Errorf("a thousand writes asked for %d scans, want one or a few", n)
	}
}

// A library at the root of a drive, as an external disk's often is, is told of changes under it.
func TestALibraryAtADrivesRootOwnsWhatIsUnderIt(t *testing.T) {
	root := filepath.VolumeName(t.TempDir()) + string(filepath.Separator)
	films := uuid.NewV7()
	w := &Watcher{roots: map[uuid.UUID]string{films: root}}
	if lib, ok := w.owner(filepath.Join(root, "Films", "Heat (1995)", "Heat.mkv")); !ok || lib != films {
		t.Errorf("a file under %s belongs to %v, %t; want the library there", root, lib, ok)
	}
}

// A library of thousands of files is watched with a handful open, not one for each: the server
// has only so many files it may open, and its sockets and FFmpeg's need them.
func TestWatchingALibraryHoldsAFewFilesOpen(t *testing.T) {
	open := func() int {
		t.Helper()
		fds, err := os.ReadDir(cmp.Or(map[string]string{"linux": "/proc/self/fd"}[runtime.GOOS], "/dev/fd"))
		if err != nil {
			t.Skip("open files are not listed here:", err)
		}
		return len(fds)
	}
	films := t.TempDir()
	for i := range 40 {
		dir := filepath.Join(films, fmt.Sprintf("Film %d (2000)", i))
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		for j := range 10 {
			if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("%d.mkv", j)), nil, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	lib := uuid.NewV7()
	f := fakeLibraries{
		libs:    []domain.Library{{ID: lib, Root: films, Monitor: domain.MonitorRealtime}},
		scanned: make(chan ask, 64),
	}
	before := open()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error)
	go func() { done <- New(f, slog.New(slog.DiscardHandler)).Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	})
	for deadline := time.After(5 * time.Second); len(f.scanned) == 0; {
		if err := os.WriteFile(filepath.Join(films, "Film 0 (2000)", "0.mkv"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
		select {
		case <-deadline:
			t.Fatal("writing queued no scan")
		case <-time.After(50 * time.Millisecond):
		}
	}
	if n := open() - before; n > 20 {
		t.Errorf("watching 40 folders of 10 files holds %d more files open, want a handful", n)
	}
}

// Finder writes .DS_Store in every folder it shows, and a NAS keeps snapshots in dot-folders:
// neither is a library's, so neither queues a scan.
func TestAChangeToWhatNoLibraryHoldsQueuesNoScan(t *testing.T) {
	films := t.TempDir()
	if err := os.Mkdir(filepath.Join(films, ".snapshot"), 0o755); err != nil {
		t.Fatal(err)
	}
	lib := uuid.NewV7()
	f := fakeLibraries{
		libs:    []domain.Library{{ID: lib, Root: films, Monitor: domain.MonitorRealtime}},
		scanned: make(chan ask, 64),
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error)
	go func() { done <- New(f, slog.New(slog.DiscardHandler)).Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	})
	// Once a write is seen, the library is watched.
	for deadline := time.After(5 * time.Second); len(f.scanned) == 0; {
		if err := os.WriteFile(filepath.Join(films, "Alien.mkv"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
		select {
		case <-deadline:
			t.Fatal("writing queued no scan")
		case <-time.After(50 * time.Millisecond):
		}
	}
	<-time.After(2 * askEvery)
	for len(f.scanned) > 0 {
		<-f.scanned
	}
	for i := range 10 {
		for _, p := range []string{filepath.Join(films, ".DS_Store"), filepath.Join(films, ".snapshot", "Alien.mkv")} {
			if err := os.WriteFile(p, []byte{byte(i)}, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		<-time.After(50 * time.Millisecond)
	}
	<-time.After(2 * askEvery)
	if n := len(f.scanned); n != 0 {
		t.Errorf("writing .DS_Store and into .snapshot queued %d scans, want none", n)
	}
}
