package blob

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/iotest"
	"time"
)

func keys(t *testing.T, d *Dir, prefix string) []string {
	t.Helper()
	var ks []string
	for e, err := range d.List(t.Context(), prefix) {
		if err != nil {
			t.Fatal(err)
		}
		ks = append(ks, e.Key)
	}
	return ks
}

func TestAnObjectIsKeptWholeOrNotAtAll(t *testing.T) {
	d, err := OpenDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	ctx := t.Context()
	if err := d.Put(ctx, "a/b/poster", strings.NewReader("poster")); err != nil {
		t.Fatal(err)
	}
	failing := io.MultiReader(strings.NewReader("half"), iotest.ErrReader(errors.New("cut off")))
	if err := d.Put(ctx, "a/b/poster", failing); err == nil {
		t.Error("a put whose reader failed succeeded")
	}
	o, err := d.Open(ctx, "a/b/poster")
	if err != nil {
		t.Fatal(err)
	}
	defer o.Close()
	if b, _ := io.ReadAll(o); string(b) != "poster" {
		t.Errorf("read %q after a failed put, want the object kept before it", b)
	}
	if _, err := d.Open(ctx, "a/b"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("opening the folder of a key: %v, want no object", err)
	}
	if ok, err := d.Exists(ctx, "a/b/backdrop"); ok || err != nil {
		t.Errorf("an object never put exists = %v, %v", ok, err)
	}
	if ks := keys(t, d, ""); !slices.Equal(ks, []string{"a/b/poster"}) {
		t.Errorf("listed %q, want only the object kept and nothing being written", ks)
	}
}

func TestObjectsAreListedByPrefixAndTheirFoldersGoWithThem(t *testing.T) {
	dir := t.TempDir()
	d, err := OpenDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	ctx := t.Context()
	for _, k := range []string{"p1/trickplay/0.jpg", "p1/trickplay/1.jpg", "p1/chapters/0.jpg", "p10/chapters/0.jpg", "top"} {
		if err := d.Put(ctx, k, strings.NewReader(k)); err != nil {
			t.Fatal(err)
		}
	}
	if ks := keys(t, d, "p1/"); !slices.Equal(ks, []string{"p1/chapters/0.jpg", "p1/trickplay/0.jpg", "p1/trickplay/1.jpg"}) {
		t.Errorf("listed %q under p1/", ks)
	}
	if ks := keys(t, d, "p1"); len(ks) != 4 {
		t.Errorf("listed %q under p1, want p10's too", ks)
	}
	if ks := keys(t, d, "nothing/"); len(ks) != 0 {
		t.Errorf("listed %q under a prefix no key has", ks)
	}
	for _, k := range keys(t, d, "p1/") {
		if err := d.Delete(ctx, k); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.Delete(ctx, "p1/never"); err != nil {
		t.Errorf("deleting what is not there: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "p1")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("p1's folder outlived its objects: %v", err)
	}
	if ks := keys(t, d, ""); !slices.Equal(ks, []string{"p10/chapters/0.jpg", "top"}) {
		t.Errorf("listed %q after deleting p1/", ks)
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
	left, _ := os.ReadDir(filepath.Join(dir, parts))
	if len(left) != 1 || left[0].Name() != "writing" {
		t.Errorf("left %v, want only the object another process may be writing", left)
	}
}
