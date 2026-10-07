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
	key := func(parts ...string) string {
		t.Helper()
		k, err := ContentKey(dir, parts)
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

// A file's hash is the one OpenSubtitles' own code makes of it, so subtitles made for that release
// are found by it; a file too short to sample has none.
func TestMovieHashIsOpenSubtitles(t *testing.T) {
	dir := t.TempDir()
	body := make([]byte, 200<<10)
	for i := range body {
		body[i] = byte((i*7 + 3) % 251)
	}
	for name, content := range map[string][]byte{"film.mkv": body, "short.mkv": body[:1000]} {
		if err := os.WriteFile(filepath.Join(dir, name), content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for name, want := range map[string]string{"film.mkv": "374866758397b267", "short.mkv": ""} {
		f, err := os.Open(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		got, err := MovieHash(f)
		_ = f.Close()
		if err != nil || got != want {
			t.Errorf("%s: %q, %v; want %q", name, got, err, want)
		}
	}
}
