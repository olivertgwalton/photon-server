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
	"github.com/olivertgwalton/photon-server/internal/jobs"
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

// filmToDownload is a store holding one film, and its title and part.
func filmToDownload(t *testing.T) (st *store.Store, item, part uuid.UUID) {
	t.Helper()
	log := slog.New(slog.DiscardHandler)
	url := storetest.FreshDatabase(t)
	if err := store.Migrate(t.Context(), url, log); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(t.Context(), url, log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
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
	p := store.Part{RelPath: "L/L.mkv", Size: 4, ModTime: time.Unix(0, 0), Facts: &domain.Facts{
		Container: "matroska,webm", Duration: 4 * time.Second, BitrateKbps: 8000, Streams: []domain.Stream{
			{Index: 0, Kind: domain.StreamVideo, Codec: "h264", Width: 1920, Height: 1080, Range: domain.RangeSDR},
			{Index: 1, Kind: domain.StreamAudio, Codec: "ac3", Channels: 6},
		},
	}}
	if _, err := st.SaveFolder(ctx, lib.ID, "L", []byte("v1"), []store.Film{{Title: "Lawrence", Folder: "L", Copies: []store.Copy{{ContentKey: []byte("k"), Parts: []store.Part{p}}}}}, nil); err != nil {
		t.Fatal(err)
	}
	cards, _, err := st.Wall(ctx, lib.ID, store.WallPage{Sort: domain.SortTitle, Limit: 1})
	if err != nil || len(cards) != 1 {
		t.Fatal(cards, err)
	}
	owner, err := st.AddProfile(ctx, "Owner", domain.RoleMember, "hash")
	if err != nil {
		t.Fatal(err)
	}
	c, err := st.Playable(ctx, owner.ID, cards[0].ID, uuid.UUID{})
	if err != nil {
		t.Fatal(err)
	}
	return st, cards[0].ID, c.Parts[0].ID
}

// slots is a node's transcode slots, at most one at once.
func slots(t *testing.T) *hls.Remuxer {
	t.Helper()
	r, err := hls.NewRemuxer("ffmpeg", t.TempDir(), t.TempDir(), hls.Hardware{Accel: domain.AccelSoftware}, 1, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// signIn signs a device in as a profile.
func signIn(t *testing.T, st *store.Store, profile uuid.UUID) uuid.UUID {
	t.Helper()
	id, err := st.CreateSession(t.Context(), store.NewSession{
		Kind: domain.SessionDevice, ProfileID: profile, TokenHash: []byte(uuid.NewV7().String()),
		DeviceName: "TV", Client: "Photon", ExpiresAt: new(time.Now().Add(time.Hour)),
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestTwoProfilesShareOneConversionUntilBothRemoveIt(t *testing.T) {
	st, item, part := filmToDownload(t)
	ctx := t.Context()
	var profiles []uuid.UUID
	for _, name := range []string{"Oliver", "Ada"} {
		p, err := st.AddProfile(ctx, name, domain.RoleMember, "hash")
		if err != nil {
			t.Fatal(err)
		}
		profiles = append(profiles, p.ID)
	}
	q := domain.Quality{MaxBitrateKbps: 2000, Codec: domain.VideoH264, Range: domain.RangeSDR}
	var downloads []store.Download
	for _, p := range profiles {
		d, _, err := st.AddDownload(ctx, p, signIn(t, st, p), item, part, &q)
		if err != nil {
			t.Fatal(err)
		}
		downloads = append(downloads, d)
	}

	runs := filepath.Join(t.TempDir(), "runs")
	dir := t.TempDir()
	conv, err := NewConversions(st, noNodes{}, slots(t), countingFFmpeg(t, runs), hls.Hardware{Accel: domain.AccelSoftware}, dir, uuid.NewV7())
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

func TestAConversionWaitsForASlotAndGivesItUpToAPlay(t *testing.T) {
	st, item, part := filmToDownload(t)
	ctx := t.Context()
	profile, err := st.AddProfile(ctx, "Oliver", domain.RoleMember, "hash")
	if err != nil {
		t.Fatal(err)
	}
	d, _, err := st.AddDownload(ctx, profile.ID, signIn(t, st, profile.ID), item, part, &domain.Quality{MaxBitrateKbps: 2000, Codec: domain.VideoH264, Range: domain.RangeSDR})
	if err != nil {
		t.Fatal(err)
	}
	// While slow exists, ffmpeg writes a little, says it is halfway, and goes on until it is killed.
	scratch := t.TempDir()
	slow, ffmpeg := filepath.Join(scratch, "slow"), filepath.Join(scratch, "ffmpeg")
	script := "#!/bin/sh\nfor a; do out=$a; done\n" +
		"if [ -e '" + slow + "' ]; then printf partial > \"$out\"; echo out_time_us=2000000; exec sleep 60; fi\n" +
		"printf converted > \"$out\"\necho out_time_us=4000000\necho progress=end\n"
	if err := os.WriteFile(ffmpeg, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	r := slots(t)
	dir := t.TempDir()
	conv, err := NewConversions(st, noNodes{}, r, ffmpeg, hls.Hardware{Accel: domain.AccelSoftware}, dir, uuid.NewV7())
	if err != nil {
		t.Fatal(err)
	}
	transcode := hls.Copy{Parts: []hls.Source{{
		Open:  func() (*os.File, error) { return nil, os.ErrNotExist },
		Part:  hls.Part{Duration: time.Minute, Keyframes: hls.Forced(time.Minute)},
		Video: domain.VideoPlan{Codec: "hevc", Encode: &domain.VideoEncode{Codec: "h264", Width: 1280, Height: 720, BitrateKbps: 4000}},
	}}}
	state := func() store.Download {
		t.Helper()
		got, err := st.Download(ctx, profile.ID, d.ID)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}

	play := uuid.NewV7()
	if err := r.Open(t.Context(), play, transcode); err != nil {
		t.Fatal(err)
	}
	if err := conv.Convert(ctx, d.Conversion); !errors.Is(err, jobs.ErrNotNow) {
		t.Fatalf("converting while a play holds the only slot: %v, want ErrNotNow", err)
	}
	if got := state(); got.State != domain.DownloadQueued {
		t.Errorf("the download while it waits: %+v, want it queued", got)
	}
	r.Close(play)

	if err := os.WriteFile(slow, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	converted := make(chan error, 1)
	go func() { converted <- conv.Convert(ctx, d.Conversion) }()
	for got := state(); got.State != domain.DownloadConverting || got.Progress != 0.5; got = state() {
		select {
		case err := <-converted:
			t.Fatalf("the conversion ended before it was halfway: %v", err)
		case <-time.After(10 * time.Millisecond):
		}
	}
	if active, conversions, _ := r.Transcodes(); active != 1 || conversions != 1 {
		t.Errorf("transcodes while converting: %d, %d of them conversions; want the one conversion", active, conversions)
	}
	second := uuid.NewV7()
	if err := r.Open(t.Context(), second, transcode); err != nil {
		t.Fatalf("a play while a conversion holds the only slot: %v, want it to take the slot", err)
	}
	if err := <-converted; !errors.Is(err, jobs.ErrNotNow) {
		t.Fatalf("the conversion a play took the slot of: %v, want ErrNotNow", err)
	}
	if got := state(); got.State != domain.DownloadQueued || got.Progress != 0 {
		t.Errorf("the download stopped for a play: %+v, want it queued from the beginning", got)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("the stopped conversion left %v", entries)
	}

	if err := os.Remove(slow); err != nil {
		t.Fatal(err)
	}
	r.Close(second)
	if err := conv.Convert(ctx, d.Conversion); err != nil {
		t.Fatalf("converting once the play ended: %v", err)
	}
	if got := state(); got.State != domain.DownloadReady || got.SizeBytes != int64(len("converted")) {
		t.Errorf("the download once converted again: %+v, want it ready", got)
	}
}

func TestARestartedNodeKeepsItsReadyDownloads(t *testing.T) {
	st, item, part := filmToDownload(t)
	ctx := t.Context()
	profile, err := st.AddProfile(ctx, "Oliver", domain.RoleMember, "hash")
	if err != nil {
		t.Fatal(err)
	}
	d, _, err := st.AddDownload(ctx, profile.ID, signIn(t, st, profile.ID), item, part, &domain.Quality{MaxBitrateKbps: 2000, Codec: domain.VideoH264, Range: domain.RangeSDR})
	if err != nil {
		t.Fatal(err)
	}
	dir, node := t.TempDir(), uuid.NewV7()
	ffmpeg := countingFFmpeg(t, filepath.Join(t.TempDir(), "runs"))
	before, err := NewConversions(st, noNodes{}, slots(t), ffmpeg, hls.Hardware{Accel: domain.AccelSoftware}, dir, node)
	if err != nil {
		t.Fatal(err)
	}
	if err := before.Convert(ctx, d.Conversion); err != nil {
		t.Fatal(err)
	}

	// The node as it starts again, with its id and its cache, prunes first.
	after, err := NewConversions(st, noNodes{}, slots(t), ffmpeg, hls.Hardware{Accel: domain.AccelSoftware}, dir, node)
	if err != nil {
		t.Fatal(err)
	}
	if err := after.Prune(ctx); err != nil {
		t.Fatal(err)
	}
	f, _, err := after.File(ctx, d.ID)
	if err != nil {
		t.Fatalf("the ready download after a restart: %v", err)
	}
	b, _ := io.ReadAll(f)
	_ = f.Close()
	if string(b) != "converted" {
		t.Errorf("its file is %q", b)
	}
}
