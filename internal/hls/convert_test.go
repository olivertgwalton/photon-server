package hls

import (
	"bytes"
	"cmp"
	"encoding/binary"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
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
	bps, err := strconv.Atoi(got.Format.BitRate)
	if err != nil {
		t.Fatalf("ffprobe's bit rate: %v", err)
	}
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

// A 10-bit HDR10 film encoded again at a lower bitrate stays HDR10: HEVC Main 10 tagged hvc1, in
// BT.2020 and PQ, with its mastering display and light levels carried over.
func TestHDR10KeptInHEVCIsStillHDR10(t *testing.T) {
	ffmpeg, ffprobe := tool(t, "ffmpeg", "PHOTON_FFMPEG"), tool(t, "ffprobe", "PHOTON_FFPROBE")
	if encoders, err := exec.CommandContext(t.Context(), ffmpeg, "-hide_banner", "-encoders").Output(); err != nil || !bytes.Contains(encoders, []byte("libx265")) {
		t.Skipf("needs an ffmpeg with libx265: %v", err)
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "film.mkv")
	made := exec.CommandContext(t.Context(), ffmpeg, "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "testsrc2=size=1920x1080:rate=24,format=yuv420p10le,setparams=color_primaries=bt2020:color_trc=smpte2084:colorspace=bt2020nc",
		"-t", "2", "-c:v", "libx265", "-preset", "ultrafast", "-profile:v", "main10", "-b:v", "20M",
		"-x265-params", "log-level=error:hdr10=1:master-display=G(13250,34500)B(7500,3000)R(34000,16000)WP(15635,16450)L(10000000,1):max-cll=1000,400", src)
	if out, err := made.CombinedOutput(); err != nil {
		t.Fatalf("making the film: %v: %s", err, out)
	}
	f, err := os.Open(src)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	dst := filepath.Join(dir, "film.mp4")
	video := domain.VideoPlan{Stream: 0, Codec: "hevc", Encode: &domain.VideoEncode{
		Codec: domain.VideoHEVC, Width: 1280, Height: 720, BitrateKbps: 2000, Range: domain.RangeHDR10,
	}}
	if err := (Hardware{Accel: domain.AccelSoftware}).Convert(t.Context(), ffmpeg, f, video, nil, 2*time.Second, dst,
		func(float64) error { return nil }); err != nil {
		t.Fatal(err)
	}
	probe, err := exec.CommandContext(t.Context(), ffprobe, "-v", "error", "-select_streams", "v", "-show_streams",
		"-show_frames", "-read_intervals", "%+#1", "-of", "json", dst).Output()
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Streams []struct {
			CodecName      string `json:"codec_name"`
			CodecTag       string `json:"codec_tag_string"`
			Profile        string `json:"profile"`
			PixFmt         string `json:"pix_fmt"`
			Width          int    `json:"width"`
			ColorTransfer  string `json:"color_transfer"`
			ColorPrimaries string `json:"color_primaries"`
		} `json:"streams"`
		Frames []struct {
			SideData []struct {
				Type string `json:"side_data_type"`
			} `json:"side_data_list"`
		} `json:"frames"`
	}
	if err := json.Unmarshal(probe, &got); err != nil {
		t.Fatal(err)
	}
	var sideData []string
	for _, fr := range got.Frames {
		for _, sd := range fr.SideData {
			sideData = append(sideData, sd.Type)
		}
	}
	if len(got.Streams) != 1 || got.Streams[0].CodecName != "hevc" || got.Streams[0].CodecTag != "hvc1" ||
		got.Streams[0].Profile != "Main 10" || got.Streams[0].PixFmt != "yuv420p10le" || got.Streams[0].Width != 1280 ||
		got.Streams[0].ColorTransfer != "smpte2084" || got.Streams[0].ColorPrimaries != "bt2020" ||
		!slices.Contains(sideData, "Mastering display metadata") || !slices.Contains(sideData, "Content light level metadata") {
		t.Errorf("ffprobe says %+v with side data %v, want 1280-wide HEVC Main 10 hvc1 in BT.2020 PQ with its mastering display and light levels", got.Streams, sideData)
	}
}
