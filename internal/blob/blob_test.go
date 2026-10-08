package blob

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"iter"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/iotest"
	"time"
)

// store is what Dir and Bucket both do.
type store interface {
	Open(ctx context.Context, key string) (Object, error)
	Exists(ctx context.Context, key string) (bool, error)
	Put(ctx context.Context, key string, r io.Reader) error
	Delete(ctx context.Context, key string) error
	List(ctx context.Context, prefix string) iter.Seq2[Entry, error]
}

func keys(t *testing.T, s store, prefix string) []string {
	t.Helper()
	var ks []string
	for e, err := range s.List(t.Context(), prefix) {
		if err != nil {
			t.Fatal(err)
		}
		ks = append(ks, e.Key)
	}
	return ks
}

func read(t *testing.T, s store, key string) []byte {
	t.Helper()
	o, err := s.Open(t.Context(), key)
	if err != nil {
		t.Fatal(err)
	}
	defer o.Close()
	b, err := io.ReadAll(o)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func newDir(t *testing.T) *Dir {
	t.Helper()
	d, err := OpenDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func TestADirKeepsObjectsWholeOrNotAtAll(t *testing.T) { testWholeOrNotAtAll(t, newDir(t)) }

func TestADirListsObjectsByPrefix(t *testing.T) { testListedByPrefix(t, newDir(t)) }

func testWholeOrNotAtAll(t *testing.T, s store) {
	ctx := t.Context()
	// One object sent in one request, and one too large to be.
	for _, size := range []int{6, partSize + 1} {
		want := bytes.Repeat([]byte("p"), size)
		if err := s.Put(ctx, "a/b/poster", bytes.NewReader(want)); err != nil {
			t.Fatal(err)
		}
		failing := io.MultiReader(bytes.NewReader(make([]byte, size)), iotest.ErrReader(errors.New("cut off")))
		if err := s.Put(ctx, "a/b/poster", failing); err == nil {
			t.Errorf("a put of %d bytes whose reader failed succeeded", size)
		}
		if got := read(t, s, "a/b/poster"); !bytes.Equal(got, want) {
			t.Errorf("read %d bytes after a failed put, want the %d put before it", len(got), size)
		}
	}
	o, err := s.Open(ctx, "a/b/poster")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := o.Seek(partSize-2, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(o)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) != 3 {
		t.Errorf("read %d bytes from 3 before the end, want 3", len(b))
	}
	_ = o.Close()
	if _, err := s.Open(ctx, "a/b"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("opening the folder of a key: %v, want no object", err)
	}
	if _, err := s.Open(ctx, "a/b/backdrop"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("opening an object never put: %v, want none", err)
	}
	if ok, err := s.Exists(ctx, "a/b/backdrop"); ok || err != nil {
		t.Errorf("an object never put exists = %v, %v", ok, err)
	}
	if ok, err := s.Exists(ctx, "a/b/poster"); !ok || err != nil {
		t.Errorf("an object put exists = %v, %v", ok, err)
	}
	if ks := keys(t, s, ""); !slices.Equal(ks, []string{"a/b/poster"}) {
		t.Errorf("listed %q, want only the object kept and nothing being written", ks)
	}
}

func testListedByPrefix(t *testing.T, s store) {
	ctx := t.Context()
	for _, k := range []string{"p1/trickplay/0.jpg", "p1/trickplay/1.jpg", "p1/chapters/0.jpg", "p10/chapters/0.jpg", "top"} {
		if err := s.Put(ctx, k, strings.NewReader(k)); err != nil {
			t.Fatal(err)
		}
	}
	if ks := keys(t, s, "p1/"); !slices.Equal(ks, []string{"p1/chapters/0.jpg", "p1/trickplay/0.jpg", "p1/trickplay/1.jpg"}) {
		t.Errorf("listed %q under p1/", ks)
	}
	if ks := keys(t, s, "p1"); len(ks) != 4 {
		t.Errorf("listed %q under p1, want p10's too", ks)
	}
	if ks := keys(t, s, "nothing/"); len(ks) != 0 {
		t.Errorf("listed %q under a prefix no key has", ks)
	}
	for _, k := range keys(t, s, "p1/") {
		if err := s.Delete(ctx, k); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Delete(ctx, "p1/never"); err != nil {
		t.Errorf("deleting what is not there: %v", err)
	}
	if ks := keys(t, s, ""); !slices.Equal(ks, []string{"p10/chapters/0.jpg", "top"}) {
		t.Errorf("listed %q after deleting p1/", ks)
	}
	for range s.List(ctx, "") {
		break // A listing stopped early ends without a fault.
	}
}

func TestAFolderGoesWithItsLastObject(t *testing.T) {
	dir := t.TempDir()
	d, err := OpenDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	for _, k := range []string{"p1/trickplay/0.jpg", "p1/chapters/0.jpg"} {
		if err := d.Put(t.Context(), k, strings.NewReader(k)); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.Delete(t.Context(), "p1/trickplay/0.jpg"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "p1", "trickplay")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("p1/trickplay outlived its objects: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "p1")); err != nil {
		t.Errorf("p1, which holds an object still: %v", err)
	}
}

func TestAnObjectAbandonedHalfWrittenIsRemovedOnOpening(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, parts), 0o750); err != nil {
		t.Fatal(err)
	}
	long := time.Now().Add(-2 * partLife)
	for name, at := range map[string]time.Time{"abandoned": long, "writing": time.Now()} {
		p := filepath.Join(dir, parts, name)
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, at, at); err != nil {
			t.Fatal(err)
		}
	}
	d, err := OpenDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	left, err := os.ReadDir(filepath.Join(dir, parts))
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 1 || left[0].Name() != "writing" {
		t.Errorf("left %v, want only the object another process may be writing", left)
	}
}
