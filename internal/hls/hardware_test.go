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
		err := Hardware{Accel: domain.AccelVAAPI, Device: "/dev/dri/renderD128"}.Check(t.Context(), ffmpeg, domain.VideoHEVC)
		if !errors.Is(err, tc.want) {
			t.Errorf("%s: %v, want %v", tc.script, err, tc.want)
		}
	}
}

// VideoToolbox refuses to decode an interlaced picture, so FFmpeg decodes it and deinterlaces it
// before the upload.
func TestEachDeviceDeinterlacesAnInterlacedPicture(t *testing.T) {
	e := domain.VideoEncode{Codec: domain.VideoH264, Width: 720, Height: 576, BitrateKbps: 4000, Range: domain.RangeSDR, Deinterlace: true}
	for accel, filter := range map[domain.Acceleration]string{
		domain.AccelSoftware: "yadif=0:-1:0,scale=", domain.AccelVideoToolbox: "yadif=0:-1:0,format=nv12|p010le,hwupload,scale_vt",
		domain.AccelVAAPI: "hwupload,deinterlace_vaapi=rate=frame,scale_vaapi", domain.AccelQSV: "vpp_qsv=w=720:h=576:format=nv12:deinterlace=2",
		domain.AccelNVENC: "hwupload,yadif_cuda=0:-1:0,scale_cuda",
	} {
		hw := Hardware{Accel: accel, Device: "d"}
		if got, _ := hw.videoArgs(e, "mpeg2video"); !strings.Contains(got, filter) {
			t.Errorf("%s: %q lacks %q", accel, got, filter)
		}
		if decoded := strings.Contains(strings.Join(hw.inputArgs("h264", e), " "), "-hwaccel "); decoded == (accel == domain.AccelSoftware || accel == domain.AccelVideoToolbox) {
			t.Errorf("%s: interlaced H.264 decoded on the device %t", accel, decoded)
		}
		progressive := e
		progressive.Deinterlace = false
		if got, _ := hw.videoArgs(progressive, "mpeg2video"); strings.Contains(got, "yadif") || strings.Contains(got, "deinterlace") {
			t.Errorf("%s: a progressive picture is deinterlaced: %q", accel, got)
		}
	}
}

// Every device encodes H.264 with a keyframe each segment, decodes H.264 and HEVC itself, and has
// anything else uploaded to it.
func TestEachDeviceEncodesTheWholeChain(t *testing.T) {
	e := domain.VideoEncode{Codec: domain.VideoH264, Width: 1280, Height: 720, BitrateKbps: 4000, Range: domain.RangeSDR, ToneMap: true}
	for accel, encoder := range map[domain.Acceleration]string{
		domain.AccelSoftware: "libx264", domain.AccelVideoToolbox: "h264_videotoolbox",
		domain.AccelVAAPI: "h264_vaapi", domain.AccelQSV: "h264_qsv", domain.AccelNVENC: "h264_nvenc",
	} {
		hw := Hardware{Accel: accel, Device: "d"}
		line := func(codec string) string {
			filter, encoder := hw.videoArgs(e, codec)
			return strings.Join(append(append(hw.inputArgs(codec, e), "-vf", filter), encoder...), " ")
		}
		hevc, av1 := line("hevc"), line("av1")
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

// HDR kept in HEVC is encoded in 10 bits on every device, as Main 10 tagged hvc1, and not tone
// mapped; SDR in HEVC is Main, in 8 bits.
func TestEachDeviceKeepsHDRInHEVC(t *testing.T) {
	hdr := domain.VideoEncode{Codec: domain.VideoHEVC, Width: 1920, Height: 1080, BitrateKbps: 8000, Range: domain.RangeHDR10}
	sdr := hdr
	sdr.Range = domain.RangeSDR
	for accel, want := range map[domain.Acceleration][2]string{
		domain.AccelSoftware: {"libx265", "format=yuv420p10le"}, domain.AccelVideoToolbox: {"hevc_videotoolbox", "scale_vt=w=1920:h=1080:format=p010"},
		domain.AccelVAAPI: {"hevc_vaapi", "scale_vaapi=w=1920:h=1080:format=p010"}, domain.AccelQSV: {"hevc_qsv", "vpp_qsv=w=1920:h=1080:format=p010"},
		domain.AccelNVENC: {"hevc_nvenc", "scale_cuda=w=1920:h=1080:format=p010"},
	} {
		hw := Hardware{Accel: accel, Device: "d"}
		filter, encoder := hw.videoArgs(hdr, "hevc")
		line := filter + " " + strings.Join(encoder, " ")
		for _, w := range []string{"-c:v " + want[0], want[1], "-profile:v main10", "-tag:v hvc1", "expr:gte(t,n_forced*6)"} {
			if !strings.Contains(line, w) {
				t.Errorf("%s: %q lacks %q", accel, line, w)
			}
		}
		if strings.Contains(line, "tonemap") {
			t.Errorf("%s: HDR kept is tone mapped: %q", accel, line)
		}
		filter, encoder = hw.videoArgs(sdr, "h264")
		if line := filter + " " + strings.Join(encoder, " "); !strings.Contains(line, "-profile:v main ") || strings.Contains(line, "p010") {
			t.Errorf("%s: SDR in HEVC is not Main in 8 bits: %q", accel, line)
		}
	}
}
