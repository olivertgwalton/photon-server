package hls

import (
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/media"
)

// A track of sound alone, encoded for a player that cannot play its FLAC, is cut into segments of
// SegmentLength, each that long, from wherever the player starts.
func TestSoundAloneIsCutIntoSegmentsOfItsLength(t *testing.T) {
	ffmpeg, ffprobe := tool(t, "ffmpeg", "PHOTON_FFMPEG"), tool(t, "ffprobe", "PHOTON_FFPROBE")
	src := filepath.Join(t.TempDir(), "track.flac")
	if out, err := exec.CommandContext(t.Context(), ffmpeg, "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "sine=frequency=440:sample_rate=44100:duration=20", src).CombinedOutput(); err != nil {
		t.Fatalf("making the track: %v: %s", err, out)
	}
	r, err := NewRemuxer(media.Tools{FFmpeg: media.Tool{Path: ffmpeg}}, t.TempDir(), t.TempDir(), Hardware{Accel: domain.AccelSoftware}, Unlimited, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	playback := uuid.NewV7()
	if err := r.Open(t.Context(), playback, Version{
		Variant: Variant{BandwidthKbps: 128, Codecs: []string{"mp4a.40.2"}},
		Parts: []Source{{
			Open: opening(src), Part: Part{Duration: 20 * time.Second, Keyframes: Forced(20 * time.Second)},
			Audio: &domain.AudioPlan{Stream: 0, Encode: &domain.AudioEncode{Codec: "aac", Channels: 2, BitrateKbps: 128}},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	defer r.Close(playback)
	master, err := r.Playlist(playback, MasterName)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(master, `CODECS="mp4a.40.2"`) || strings.Contains(master, "RESOLUTION") {
		t.Errorf("master playlist:\n%s\nwant a variant of AAC alone", master)
	}
	playlist, err := r.Resource(t.Context(), playback, videoName)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"#EXTINF:6.000000,\n", "#EXTINF:2.000000,\n"} {
		if !strings.Contains(playlist.Text, want) {
			t.Errorf("media playlist:\n%s\nwant %q", playlist.Text, want)
		}
	}
	// The second segment, asked for first as a player seeking to it would, holds its six seconds.
	init, err := r.Init(t.Context(), playback, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer init.Close()
	seg, err := r.Segment(t.Context(), playback, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer seg.Close()
	joined := filepath.Join(t.TempDir(), "joined.mp4")
	out, err := os.Create(joined)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []io.Reader{init, seg} {
		if _, err := io.Copy(out, f); err != nil {
			t.Fatal(err)
		}
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
	probed, err := exec.CommandContext(t.Context(), ffprobe, "-v", "error", "-show_entries", "stream=codec_name,codec_type",
		"-of", "default=noprint_wrappers=1", joined).Output()
	if err != nil {
		t.Fatalf("probing the segment: %v", err)
	}
	if got := strings.Fields(string(probed)); strings.Join(got, " ") != "codec_name=aac codec_type=audio" {
		t.Errorf("segment 1 holds %v, want AAC alone", got)
	}
	// Its packets are shown from 6 s to 12 s of the track, on the clock offset by clockOffset.
	packets, err := exec.CommandContext(t.Context(), ffprobe, "-v", "error", "-show_entries", "packet=pts_time",
		"-of", "csv=p=0", joined).Output()
	if err != nil {
		t.Fatalf("probing the segment's packets: %v", err)
	}
	times := strings.Fields(string(packets))
	first, err := strconv.ParseFloat(times[0], 64)
	if err != nil {
		t.Fatal(err)
	}
	last, err := strconv.ParseFloat(times[len(times)-1], 64)
	if err != nil {
		t.Fatal(err)
	}
	start, end := (6*time.Second + clockOffset).Seconds(), (12*time.Second + clockOffset).Seconds()
	if first < start-0.05 || first > start+0.05 || last < end-0.1 || last > end {
		t.Errorf("segment 1 is shown from %.3f s to %.3f s, want %.0f to %.0f", first, last, start, end)
	}
}
