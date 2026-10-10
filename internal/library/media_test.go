package library

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/olivertgwalton/photon-server/internal/media"
)

func TestAStrmIsReadAsTheAddressItNames(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, map[string]string{
		"Heat (1995).strm":   "#EXTM3U\n\n  https://debrid.example/dl/heat.mkv?token=abc \r\n",
		"Alien (1979).strm":  "/etc/passwd\n",
		"Ronin (1998).strm":  "file:///media/Ronin.mkv\n",
		"Fargo (1996).strm":  "rtsp://camera.example/live\n",
		"Empty (2000).strm":  "# nothing here\n\n",
		"Brazil (1985).mkv":  "media bytes",
		"Casino (1995).STRM": "http://media.example/casino.mp4",
	})
	for rel, want := range map[string]string{
		"Heat (1995).strm":   "https://debrid.example/dl/heat.mkv?token=abc",
		"Casino (1995).STRM": "http://media.example/casino.mp4",
		"Brazil (1985).mkv":  "",
	} {
		in, err := OpenMedia(dir, rel)
		if err != nil {
			t.Fatalf("%s: %v", rel, err)
		}
		in.Close()
		got := ""
		if in.URL != nil {
			got = in.URL.String()
		}
		if got != want {
			t.Errorf("%s reads as %q, want %q", rel, got, want)
		}
	}
	for _, rel := range []string{"Alien (1979).strm", "Ronin (1998).strm", "Fargo (1996).strm", "Empty (2000).strm"} {
		if _, err := OpenMedia(dir, rel); !errors.Is(err, media.ErrNotMedia) {
			t.Errorf("%s: %v, want it not media: only an http or https address is fetched", rel, err)
		}
	}
}

// A file being played can be renamed and deleted, as Sonarr and Radarr do to upgrade it, on
// Windows as on Linux and macOS.
func TestAFileBeingReadCanBeRenamedAndDeleted(t *testing.T) {
	root := t.TempDir()
	write(t, root, map[string]string{"Film (2010)/Film (2010).mkv": "media"})
	f, err := Open(root, "Film (2010)/Film (2010).mkv")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	moved := filepath.Join(root, "Film (2010)", "Film (2010) - 1080p.mkv")
	if err := os.Rename(filepath.Join(root, "Film (2010)", "Film (2010).mkv"), moved); err != nil {
		t.Fatalf("renamed while open: %v", err)
	}
	if err := os.Remove(moved); err != nil {
		t.Fatalf("deleted while open: %v", err)
	}
	if b, err := io.ReadAll(f); err != nil || string(b) != "media" {
		t.Errorf("read %q, %v; want what was opened", b, err)
	}
}
