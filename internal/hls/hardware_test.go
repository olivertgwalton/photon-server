package hls

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

func TestADeviceThatWillNotEncodeIsFoundAtStart(t *testing.T) {
	for _, tc := range []struct {
		script string
		want   error
	}{{"exit 0", nil}, {"echo 'No VA display found' >&2; exit 1", errHardware}} {
		ffmpeg := filepath.Join(t.TempDir(), "ffmpeg")
		if err := os.WriteFile(ffmpeg, []byte("#!/bin/sh\n"+tc.script+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		err := Hardware{Accel: domain.AccelVAAPI, Device: "/dev/dri/renderD128"}.Check(t.Context(), ffmpeg)
		if !errors.Is(err, tc.want) {
			t.Errorf("%s: %v, want %v", tc.script, err, tc.want)
		}
	}
}

// Every device encodes H.264 with a keyframe each segment, decodes H.264 and HEVC itself, and has
// anything else uploaded to it.
func TestEachDeviceEncodesTheWholeChain(t *testing.T) {
	e := domain.VideoEncode{Codec: "h264", Width: 1280, Height: 720, BitrateKbps: 4000, ToneMap: true}
	for accel, encoder := range map[domain.Acceleration]string{
		domain.AccelSoftware: "libx264", domain.AccelVideoToolbox: "h264_videotoolbox",
		domain.AccelVAAPI: "h264_vaapi", domain.AccelQSV: "h264_qsv", domain.AccelNVENC: "h264_nvenc",
	} {
		hw := Hardware{Accel: accel, Device: "d"}
		hevc := strings.Join(append(hw.inputArgs("hevc"), hw.videoArgs(e, "hevc")...), " ")
		av1 := strings.Join(append(hw.inputArgs("av1"), hw.videoArgs(e, "av1")...), " ")
		for _, want := range []string{"-c:v " + encoder, "expr:gte(t,n_forced*6)", "1280", "tonemap"} {
			if !strings.Contains(hevc, want) {
				t.Errorf("%s: %q lacks %q", accel, hevc, want)
			}
		}
		if accel == domain.AccelSoftware {
			continue
		}
		if !strings.Contains(hevc, "-hwaccel ") || strings.Contains(hevc, "hwupload") {
			t.Errorf("%s: HEVC is not decoded on the device: %q", accel, hevc)
		}
		if strings.Contains(av1, "-hwaccel ") || !strings.Contains(av1, "hwupload") {
			t.Errorf("%s: AV1 is not decoded by FFmpeg and uploaded: %q", accel, av1)
		}
	}
}
