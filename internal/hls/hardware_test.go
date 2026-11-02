package hls

import (
	"errors"
	"os"
	"runtime"
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
	qsv := "vpp_qsv=w=720:h=576:format=nv12:deinterlace=2"
	if runtime.GOOS == "windows" {
		qsv = "vpp_qsv=w=720:h=576:format=nv12:passthrough=0:deinterlace=2"
	}
	for accel, filter := range map[domain.Acceleration]string{
		domain.AccelSoftware: "yadif=0:-1:0,scale=", domain.AccelVideoToolbox: "yadif=0:-1:0,format=nv12|p010le|videotoolbox_vld,hwupload,scale_vt",
		domain.AccelVAAPI: "hwupload=derive_device=vaapi,deinterlace_vaapi=rate=frame,scale_vaapi", domain.AccelQSV: qsv,
		domain.AccelNVENC: "hwupload,yadif_cuda=0:-1:0,scale_cuda", domain.AccelAMF: "yadif=0:-1:0,scale=",
	} {
		hw := Hardware{Accel: accel, Device: "d"}
		if got, _ := hw.videoArgs(e); !strings.Contains(got, filter) {
			t.Errorf("%s: %q lacks %q", accel, got, filter)
		}
		if decoded := strings.Contains(strings.Join(hw.inputArgs(e), " "), "-hwaccel "); decoded == (accel == domain.AccelSoftware || accel == domain.AccelVideoToolbox || accel == domain.AccelAMF) {
			t.Errorf("%s: interlaced H.264 decoded on the device %t", accel, decoded)
		}
		progressive := e
		progressive.Deinterlace = false
		if got, _ := hw.videoArgs(progressive); strings.Contains(got, "yadif") || strings.Contains(got, "deinterlace") {
			t.Errorf("%s: a progressive picture is deinterlaced: %q", accel, got)
		}
	}
}

// Every device encodes H.264 with a keyframe each segment, and is asked to decode the source too:
// what it decodes stays on it, in the format its frames are, and what FFmpeg decodes in its stead,
// as a codec or bit depth it has no decoder for, is uploaded. AMF is given what FFmpeg decodes.
func TestEachDeviceEncodesTheWholeChain(t *testing.T) {
	e := domain.VideoEncode{Codec: domain.VideoH264, Width: 1280, Height: 720, BitrateKbps: 4000, Range: domain.RangeSDR, ToneMap: true}
	// QSV decodes through VAAPI on Linux and D3D11 on Windows.
	qsv := "format=nv12|p010le|vaapi,hwupload=derive_device=vaapi:extra_hw_frames=64,hwmap=derive_device=qsv,"
	if runtime.GOOS == "windows" {
		qsv = "format=nv12|p010le|d3d11,hwupload=derive_device=d3d11va:extra_hw_frames=64,hwmap=derive_device=qsv,"
	}
	for accel, want := range map[domain.Acceleration][2]string{
		domain.AccelSoftware: {"libx264"}, domain.AccelAMF: {"h264_amf"},
		domain.AccelVideoToolbox: {"h264_videotoolbox", "format=nv12|p010le|videotoolbox_vld,hwupload,"},
		domain.AccelVAAPI:        {"h264_vaapi", "format=nv12|p010le|vaapi,hwupload=derive_device=vaapi,"},
		domain.AccelQSV:          {"h264_qsv", qsv},
		domain.AccelNVENC:        {"h264_nvenc", "format=nv12|p010le|cuda,hwupload,"},
	} {
		hw := Hardware{Accel: accel, Device: "d"}
		filter, encoder := hw.videoArgs(e)
		line := strings.Join(append(append(hw.inputArgs(e), "-vf", filter), encoder...), " ")
		for _, w := range []string{"-c:v " + want[0], "expr:gte(t,n_forced*6)", "1280", "tonemap", want[1]} {
			if !strings.Contains(line, w) {
				t.Errorf("%s: %q lacks %q", accel, line, w)
			}
		}
		if decoded := strings.Contains(line, "-hwaccel "); decoded == (accel == domain.AccelSoftware || accel == domain.AccelAMF) {
			t.Errorf("%s: asked to decode %t: %q", accel, decoded, line)
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
		filter, encoder := hw.videoArgs(hdr)
		line := filter + " " + strings.Join(encoder, " ")
		for _, w := range []string{"-c:v " + want[0], want[1], "-profile:v main10", "-tag:v hvc1", "expr:gte(t,n_forced*6)"} {
			if !strings.Contains(line, w) {
				t.Errorf("%s: %q lacks %q", accel, line, w)
			}
		}
		if strings.Contains(line, "tonemap") {
			t.Errorf("%s: HDR kept is tone mapped: %q", accel, line)
		}
		filter, encoder = hw.videoArgs(sdr)
		if line := filter + " " + strings.Join(encoder, " "); !strings.Contains(line, "-profile:v main ") || strings.Contains(line, ":format=p010") {
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

// A subtitle is burned into the test picture through each device's own overlay, the subtitle the
// source's second output: SDR in H.264, as it is and tone mapped, and HDR kept in 10-bit HEVC where the device encodes HEVC,
// which Vulkan's lays over on the CPU.
func TestEachDeviceBurnsASubtitleInAtStart(t *testing.T) {
	for _, tc := range []struct {
		hw           Hardware
		overlay, hdr string
	}{
		{Hardware{Accel: domain.AccelSoftware}, "[main][sub]overlay=", ""},
		{Hardware{Accel: domain.AccelVideoToolbox}, "[main][sub]overlay_videotoolbox=", ""},
		{Hardware{Accel: domain.AccelQSV, Device: "/dev/dri/renderD128"}, "[main][sub]overlay_qsv=", ""},
		{Hardware{Accel: domain.AccelNVENC, Device: "0"}, "[main][sub]overlay_cuda=", ""},
		{Hardware{Accel: domain.AccelAMF}, "[main][sub]overlay=", ""},
		{Hardware{Accel: domain.AccelVAAPI, Device: "/dev/dri/renderD128", Overlay: OverlayVAAPI}, "[main][sub]overlay_vaapi=", ""},
		{Hardware{Accel: domain.AccelVAAPI, Device: "/dev/dri/renderD128", Overlay: OverlayVulkan}, "[main][sub]overlay_vulkan=", "[main][sub]overlay="},
		{Hardware{Accel: domain.AccelVAAPI, Device: "/dev/dri/renderD128", Overlay: OverlayCPU}, "[main][sub]overlay=", ""},
	} {
		for hevc, runs := range map[domain.HEVCEncoding]int{domain.HEVCAllow: 3, domain.HEVCDeny: 2} {
			dir := t.TempDir()
			ffmpeg := testtool.Script(t, dir, "ffmpeg", `echo "$*" >> "`+dir+`/runs"`+"\n")
			hw := tc.hw
			hw.HEVC = hevc
			if err := hw.CheckBurn(t.Context(), ffmpeg); err != nil {
				t.Fatal(err)
			}
			b, err := os.ReadFile(dir + "/runs")
			if err != nil {
				t.Fatal(err)
			}
			lines := strings.Split(strings.TrimSpace(string(b)), "\n")
			if len(lines) != runs {
				t.Errorf("%s %s, HEVC %s: %d runs, want %d", hw.Accel, hw.Overlay, hevc, len(lines), runs)
			}
			for i, l := range lines {
				overlay := tc.overlay
				if i == 2 && tc.hdr != "" {
					overlay = tc.hdr
				}
				for _, w := range []string{"[out0];color=c=white@0.5:size=320x60:rate=24,format=yuva420p[out1]", "[0:1]scale=640:360", overlay} {
					if !strings.Contains(l, w) {
						t.Errorf("%s %s: %q lacks %q", hw.Accel, hw.Overlay, l, w)
					}
				}
			}
			if hevc == domain.HEVCAllow && !strings.Contains(lines[2], "p010") && !strings.Contains(lines[2], "yuv420p10") {
				t.Errorf("%s %s: HDR kept is not burned in 10 bits: %q", hw.Accel, hw.Overlay, lines[2])
			}
		}
	}
}

// A driver's tone mapper is decided as its overlay is: Intel's iHD maps on the device, AMD's
// through Vulkan where Vulkan imports its frames, and anything else on the CPU.
func TestAVAAPIDevicesToneMapperIsChosenByItsDriver(t *testing.T) {
	for overlay, want := range map[Overlay]ToneMapper{
		OverlayVAAPI: ToneMapperVAAPI, OverlayVulkan: ToneMapperVulkan, OverlayCPU: ToneMapperCPU,
	} {
		if got := ToneMapperOf(overlay); got != want {
			t.Errorf("%s: %s, want %s", overlay, got, want)
		}
	}
}

// Each VAAPI tone mapper maps HDR to 8-bit SDR at the size encoded and hands the encoder a VAAPI
// picture: Vulkan's with VAAPI and Vulkan opened on one DRM device, the CPU's scaling on the device
// before it maps.
func TestEachVAAPIToneMapperMapsTheWholeChain(t *testing.T) {
	e := domain.VideoEncode{Codec: domain.VideoH264, Width: 1920, Height: 1080, BitrateKbps: 8000, Range: domain.RangeSDR, ToneMap: true}
	const dev = "-init_hw_device vaapi=hw:/dev/dri/renderD128 -filter_hw_device hw "
	const vk = "-init_hw_device drm=dr:/dev/dri/renderD128 -init_hw_device vaapi=hw@dr -init_hw_device vulkan=vk@dr -filter_hw_device vk "
	for _, tc := range []struct {
		mapper       ToneMapper
		init, filter string
	}{
		{ToneMapperVAAPI, dev, "format=nv12|p010le|vaapi,hwupload=derive_device=vaapi,tonemap_vaapi=format=nv12:p=bt709:t=bt709:m=bt709,scale_vaapi=w=1920:h=1080"},
		{ToneMapperVulkan, vk, "format=nv12|p010le|vaapi,hwupload=derive_device=vaapi,hwmap=derive_device=drm,format=drm_prime," +
			"libplacebo=upscaler=none:downscaler=none:format=bgra:range=pc:w=1920:h=1080:tonemapping=bt.2390:peak_detect=0:color_primaries=bt709:color_trc=bt709:colorspace=bt709," +
			"format=vulkan,hwmap=derive_device=vaapi,format=vaapi,scale_vaapi=format=nv12:out_range=tv"},
		{ToneMapperCPU, dev, "format=nv12|p010le|vaapi,hwupload=derive_device=vaapi,scale_vaapi=w=1920:h=1080:format=p010,hwdownload,format=p010le," +
			"tonemapx=tonemap=bt2390:desat=0:peak=100:t=bt709:m=bt709:p=bt709:format=nv12,hwupload=derive_device=vaapi"},
	} {
		hw := Hardware{Accel: domain.AccelVAAPI, Device: "/dev/dri/renderD128", Overlay: OverlayCPU, ToneMapper: tc.mapper}
		if init := strings.Join(hw.inputArgs(e), " "); !strings.HasPrefix(init, tc.init) {
			t.Errorf("%s: opens %q, want %q", tc.mapper, init, tc.init)
		}
		if filter, _ := hw.videoArgs(e); filter != tc.filter {
			t.Errorf("%s: %q, want %q", tc.mapper, filter, tc.filter)
		}
		kept := e
		kept.ToneMap, kept.Range, kept.Codec = false, domain.RangeHDR10, domain.VideoHEVC
		if init := strings.Join(hw.inputArgs(kept), " "); !strings.HasPrefix(init, dev) {
			t.Errorf("%s: HDR kept opens %q", tc.mapper, init)
		}
	}
}
