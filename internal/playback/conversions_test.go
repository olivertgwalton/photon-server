//go:build integration

package playback

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/hls"
	"github.com/olivertgwalton/photon-server/internal/media"
	"github.com/olivertgwalton/photon-server/internal/store"
	"github.com/olivertgwalton/photon-server/internal/store/storetest"
)

// countingFFmpeg writes "converted" wherever it is told to and notes each run in runs.
func countingFFmpeg(t *testing.T, runs string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ffmpeg")
	script := "#!/bin/sh\necho run >> '" + runs + "'\nfor a; do out=$a; done\nprintf converted > \"$out\"\necho out_time_us=4000000\necho progress=end\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

type noNodes struct{}

func (noNodes) NodeAddress(context.Context, uuid.UUID) (string, bool, error) { return "", false, nil }

func TestTwoProfilesShareOneConversionUntilBothRemoveIt(t *testing.T) {
	log := slog.New(slog.DiscardHandler)
	url := storetest.FreshDatabase(t)
	if err := store.Migrate(t.Context(), url, log); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(t.Context(), url, log)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := t.Context()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "L"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "L", "L.mkv"), []byte("film"), 0o600); err != nil {
		t.Fatal(err)
	}
	lib, err := st.AddLibrary(ctx, "Films", domain.LibraryMovies, root)
	if err != nil {
		t.Fatal(err)
	}
	part := store.Part{RelPath: "L/L.mkv", Size: 4, ModTime: time.Unix(0, 0), Facts: &media.Facts{
		Container: "matroska,webm", Duration: 4 * time.Second, BitrateKbps: 8000, Streams: []media.Stream{
			{Index: 0, Kind: domain.StreamVideo, Codec: "h264", Width: 1920, Height: 1080, Range: domain.RangeSDR},
			{Index: 1, Kind: domain.StreamAudio, Codec: "ac3", Channels: 6},
		},
	}}
	if _, err := st.SaveFolder(ctx, lib.ID, "L", []byte("v1"), []store.Film{{Title: "Lawrence", Folder: "L", Copies: []store.Copy{{ContentKey: []byte("k"), Parts: []store.Part{part}}}}}, nil); err != nil {
		t.Fatal(err)
	}
	cards, _, err := st.Wall(ctx, lib.ID, store.WallPage{Sort: domain.SortTitle, Limit: 1})
	if err != nil || len(cards) != 1 {
		t.Fatal(cards, err)
	}
	var profiles []uuid.UUID
	for _, name := range []string{"Oliver", "Ada"} {
		p, err := st.AddProfile(ctx, name, domain.RoleMember, "")
		if err != nil {
			t.Fatal(err)
		}
		profiles = append(profiles, p.ID)
	}
	c, err := st.Playable(ctx, profiles[0], cards[0].ID, uuid.UUID{})
	if err != nil {
		t.Fatal(err)
	}
	q := domain.Quality{MaxBitrateKbps: 2000}
	var downloads []store.Download
	for _, p := range profiles {
		d, err := st.AddDownload(ctx, p, cards[0].ID, c.Parts[0].ID, &q)
		if err != nil {
			t.Fatal(err)
		}
		downloads = append(downloads, d)
	}

	runs := filepath.Join(t.TempDir(), "runs")
	dir := t.TempDir()
	conv, err := NewConversions(st, noNodes{}, countingFFmpeg(t, runs), hls.Hardware{Accel: domain.AccelSoftware}, dir, uuid.NewV7())
	if err != nil {
		t.Fatal(err)
	}
	// The second is the job run again, as one asked for while it ran is.
	for range 2 {
		if err := conv.Convert(ctx, downloads[0].Conversion); err != nil {
			t.Fatal(err)
		}
	}
	if b, _ := os.ReadFile(runs); strings.Count(string(b), "run") != 1 {
		t.Errorf("ffmpeg ran %d times, want once for both profiles", strings.Count(string(b), "run"))
	}
	for n, d := range downloads {
		got, err := st.Download(ctx, profiles[n], d.ID)
		if err != nil || got.State != domain.DownloadReady || got.SizeBytes != int64(len("converted")) {
			t.Errorf("profile %d's download: %+v, %v; want it ready", n, got, err)
		}
		f, _, err := conv.File(ctx, d.ID)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(f)
		_ = f.Close()
		if string(b) != "converted" {
			t.Errorf("profile %d's file is %q", n, b)
		}
	}
	file := filepath.Join(dir, downloads[0].Conversion.String()+".mp4")
	if err := conv.Remove(ctx, profiles[0], downloads[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(file); err != nil {
		t.Errorf("with Ada's download left the file is gone: %v", err)
	}
	if err := conv.Remove(ctx, profiles[1], downloads[1].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(file); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("with no download left the file stays: %v", err)
	}
}
