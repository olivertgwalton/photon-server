package hls

import (
	"cmp"
	"encoding/binary"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// tool finds a real ffmpeg or ffprobe, which CI does not have.
func tool(t *testing.T, name, env string) string {
	t.Helper()
	path, err := exec.LookPath(cmp.Or(os.Getenv(env), name))
	if err != nil {
		t.Skipf("needs %s: %v", name, err)
	}
	return path
}

// A short film made at 8 Mbps comes out as a smaller MP4 a player starts at once: H.264 at
// the bitrate and size asked for, AAC, its index ahead of its pictures.
func TestAConversionIsAPlayableMP4AtTheBitrateAsked(t *testing.T) {
	ffmpeg, ffprobe := tool(t, "ffmpeg", "PHOTON_FFMPEG"), tool(t, "ffprobe", "PHOTON_FFPROBE")
	dir := t.TempDir()
	src := filepath.Join(dir, "film.mkv")
	made := exec.CommandContext(t.Context(), ffmpeg, "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "testsrc2=size=1280x720:rate=24", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000",
		"-t", "4", "-c:v", "libx264", "-b:v", "8M", "-c:a", "flac", src)
	if out, err := made.CombinedOutput(); err != nil {
		t.Fatalf("making the film: %v: %s", err, out)
	}
	f, err := os.Open(src)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	dst := filepath.Join(dir, "film.mp4")
	var last float64
	video := domain.VideoPlan{Stream: 0, Codec: "h264", Encode: &domain.VideoEncode{Codec: "h264", Width: 640, Height: 360, BitrateKbps: 500}}
	audio := &domain.AudioPlan{Stream: 1, Encode: &domain.AudioEncode{Codec: "aac", Channels: 2, BitrateKbps: 128}}
	if err := (Hardware{Accel: domain.AccelSoftware}).Convert(t.Context(), ffmpeg, f, video, audio, 4*time.Second, dst,
		func(p float64) error { last = p; return nil }); err != nil {
		t.Fatal(err)
	}
	if last < 0.9 {
		t.Errorf("progress ended at %.2f, want about 1", last)
	}
	probe, err := exec.CommandContext(t.Context(), ffprobe, "-v", "error", "-show_format", "-show_streams", "-of", "json", dst).Output()
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Format struct {
			FormatName string `json:"format_name"`
			BitRate    string `json:"bit_rate"`
		} `json:"format"`
		Streams []struct {
			CodecName string `json:"codec_name"`
			Width     int    `json:"width"`
			Height    int    `json:"height"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(probe, &got); err != nil {
		t.Fatal(err)
	}
	bps, _ := strconv.Atoi(got.Format.BitRate)
	if got.Format.FormatName != "mov,mp4,m4a,3gp,3g2,mj2" || len(got.Streams) != 2 ||
		got.Streams[0].CodecName != "h264" || got.Streams[0].Width != 640 || got.Streams[0].Height != 360 ||
		got.Streams[1].CodecName != "aac" || bps == 0 || bps > 1_000_000 {
		t.Errorf("ffprobe says %+v, want H.264 640x360 and AAC in MP4 at no more than about 628 kbps", got)
	}
	b, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	var boxes []string
	for at := 0; at+8 <= len(b) && len(boxes) < 3; {
		boxes = append(boxes, string(b[at+4:at+8]))
		size := int(binary.BigEndian.Uint32(b[at:]))
		if size < 8 {
			break
		}
		at += size
	}
	if len(boxes) < 2 || boxes[1] != "moov" {
		t.Errorf("top-level boxes start %v, want the moov straight after ftyp", boxes)
	}
}
