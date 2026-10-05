package library

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestContentKey(t *testing.T) {
	dir := t.TempDir()
	body := bytes.Repeat([]byte("frame"), 100_000) // past both 64 KiB samples
	write(t, dir, map[string]string{
		"Heat (1995)/Heat (1995).mkv": string(body),
		"Alien (1979)/Alien.mkv":      string(append(append([]byte(nil), body[:len(body)-1]...), 'X')),
		"Short.mkv":                   "tiny",
	})
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	key := func(parts ...string) string {
		t.Helper()
		k, err := ContentKey(root, parts)
		if err != nil {
			t.Fatal(err)
		}
		return string(k)
	}

	heat := key("Heat (1995)/Heat (1995).mkv")
	if err := os.Rename(filepath.Join(dir, "Heat (1995)"), filepath.Join(dir, "Heat")); err != nil {
		t.Fatal(err)
	}
	if key("Heat/Heat (1995).mkv") != heat {
		t.Error("moving a file changed its key")
	}
	if key("Alien (1979)/Alien.mkv") == heat {
		t.Error("files differing in their last byte share a key")
	}
	if key("Heat/Heat (1995).mkv", "Short.mkv") == heat {
		t.Error("a two-part copy shares a key with its first part alone")
	}
	key("Short.mkv")
}
