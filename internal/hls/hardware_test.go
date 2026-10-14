package hls

import (
	"errors"
	"strings"
	"testing"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/testtool"
)

func TestADeviceThatWillNotEncodeIsFoundAtStart(t *testing.T) {
	for _, tc := range []struct {
		script string
		want   error
	}{{"exit 0", nil}, {"echo 'No VA display found' >&2; exit 1", errHardware}} {
		ffmpeg := testtool.Script(t, t.TempDir(), "ffmpeg", tc.script+"\n")
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
		domain.AccelNVENC: "hwupload,yadif_cuda=0:-1:0,scale_cuda", domain.AccelAMF: "yadif=0:-1:0,scale=",
	} {
		hw := Hardware{Accel: accel, Device: "d"}
		if got, _ := hw.videoArgs(e, "mpeg2video"); !strings.Contains(got, filter) {
			t.Errorf("%s: %q lacks %q", accel, got, filter)
		}
		if decoded := strings.Contains(strings.Join(hw.inputArgs("h264", e), " "), "-hwaccel "); decoded == (accel == domain.AccelSoftware || accel == domain.AccelVideoToolbox || accel == domain.AccelAMF) {
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
// anything else uploaded to it; AMF is given what FFmpeg decodes.
func TestEachDeviceEncodesTheWholeChain(t *testing.T) {
	e := domain.VideoEncode{Codec: domain.VideoH264, Width: 1280, Height: 720, BitrateKbps: 4000, Range: domain.RangeSDR, ToneMap: true}
	for accel, encoder := range map[domain.Acceleration]string{
		domain.AccelSoftware: "libx264", domain.AccelVideoToolbox: "h264_videotoolbox",
		domain.AccelVAAPI: "h264_vaapi", domain.AccelQSV: "h264_qsv", domain.AccelNVENC: "h264_nvenc",
		domain.AccelAMF: "h264_amf",
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
		if accel == domain.AccelSoftware || accel == domain.AccelAMF {
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
		domain.AccelNVENC: {"hevc_nvenc", "scale_cuda=w=1920:h=1080:format=p010"}, domain.AccelAMF: {"hevc_amf", "format=yuv420p10le"},
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

// What FFmpeg says opening a device, its lines as reported for Intel's iHD (immich#18001)
// and i965 (intel-vaapi-driver#480) drivers, AMD's Mesa one (mpv#17672), and a Vulkan device's
// extensions (jellyfin#17144).
const (
	ihdSaid    = "[AVHWDeviceContext @ 0x43c7a020480] VAAPI driver: Intel iHD driver for Intel(R) Gen Graphics - 24.4.2 (b6b411f).\n"
	i965Said   = "[AVHWDeviceContext @ 0xbd4580] VAAPI driver: Intel i965 driver for Intel(R) Broxton - 2.4.0.pre1 (2.3.0-18-geefe4be).\n"
	mesaSaid   = "[AVHWDeviceContext @ 0x55e218b198c0] VAAPI driver: Mesa Gallium driver 26.0.3 for AMD Radeon RX 7700 XT (radeonsi, navi32, ACO, DRM 3.64, 7.0.0-rc6).\n"
	vulkanSaid = "[AVHWDeviceContext @ 0x55e218b198c0] Using device extension VK_KHR_push_descriptor\n" +
		"[AVHWDeviceContext @ 0x55e218b198c0] Using device extension VK_KHR_external_memory_fd\n" +
		"[AVHWDeviceContext @ 0x55e218b198c0] Using device extension VK_EXT_external_memory_dma_buf\n" +
		"[AVHWDeviceContext @ 0x55e218b198c0] Using device extension VK_KHR_external_semaphore_fd\n" +
		"[AVHWDeviceContext @ 0x55e218b198c0] Using device extension VK_EXT_external_memory_host\n" +
		"[AVHWDeviceContext @ 0x55e218b198c0] Using device extension VK_EXT_image_drm_format_modifier\n"
)

// Intel's iHD driver overlays on the device, AMD's through Vulkan where Vulkan imports its frames
// on Linux 5.15 or later, and anything else on the CPU.
func TestAVAAPIDevicesOverlayIsChosenByItsDriver(t *testing.T) {
	noModifier := strings.ReplaceAll(vulkanSaid, "VK_EXT_image_drm_format_modifier", "VK_KHR_video_queue")
	for _, tc := range []struct {
		name, vaapi, vulkan, kernel string
		want                        Overlay
	}{
		{"iHD", ihdSaid, "", "", OverlayVAAPI},
		{"i965", i965Said, "", "6.8.0-45-generic\n", OverlayCPU},
		{"radeonsi", mesaSaid, vulkanSaid, "6.12.33-production+truenas\n", OverlayVulkan},
		{"radeonsi on 5.15", mesaSaid, vulkanSaid, "5.15.0-122-generic\n", OverlayVulkan},
		{"radeonsi on 5.10", mesaSaid, vulkanSaid, "5.10.0-28-amd64\n", OverlayCPU},
		{"radeonsi, Vulkan without format modifiers", mesaSaid, noModifier, "6.8.0\n", OverlayCPU},
		{"radeonsi, no Vulkan device", mesaSaid, "", "6.8.0\n", OverlayCPU},
		{"radeonsi, kernel unread", mesaSaid, vulkanSaid, "", OverlayCPU},
		{"no device", "", "", "6.8.0\n", OverlayCPU},
	} {
		if got := overlayOf(tc.vaapi, tc.vulkan, tc.kernel); got != tc.want {
			t.Errorf("%s: %s, want %s", tc.name, got, tc.want)
		}
	}
}

// A device FFmpeg fails to open says nothing of its driver.
func TestADeviceThatWillNotOpenOverlaysOnTheCPU(t *testing.T) {
	for script, want := range map[string]Overlay{
		"echo '" + strings.TrimSpace(ihdSaid) + "' >&2\n":         OverlayVAAPI,
		"echo '" + strings.TrimSpace(ihdSaid) + "' >&2\nexit 1\n": OverlayCPU,
	} {
		ffmpeg := testtool.Script(t, t.TempDir(), "ffmpeg", script)
		if got := ProbeOverlay(t.Context(), ffmpeg, "/dev/dri/renderD128"); got != want {
			t.Errorf("%q: %s, want %s", script, got, want)
		}
	}
}
