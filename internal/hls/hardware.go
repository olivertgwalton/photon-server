package hls

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/media"
)

// Hardware is the device video is encoded on, chosen for the whole server as Jellyfin's is:
// VideoToolbox on a Mac, VAAPI or QSV on an Intel or AMD render node, NVENC on an NVIDIA card,
// and on Windows QSV on an Intel GPU or AMF on an AMD one.
type Hardware struct {
	Accel domain.Acceleration
	// Device is the VAAPI or QSV render node, or the CUDA device's index; on Windows, where QSV
	// opens the Intel GPU through D3D11, none.
	Device string
	// HEVC is whether video is encoded to HEVC for a client that plays it.
	HEVC domain.HEVCEncoding
	// Overlay is how VAAPI lays a burned-in subtitle over the picture.
	Overlay Overlay
}

// Overlay is how a VAAPI device lays a subtitle drawn on the CPU over the picture, chosen by its
// driver as Jellyfin's pipelines are (see ProbeOverlay).
type Overlay string

const (
	// OverlayVAAPI is the driver's own overlay_vaapi: Intel's iHD.
	OverlayVAAPI Overlay = "vaapi"
	// OverlayVulkan maps the picture to Vulkan for overlay_vulkan and back: AMD's Mesa driver, where
	// Vulkan imports its frames.
	OverlayVulkan Overlay = "vulkan"
	// OverlayCPU takes the picture off the device for FFmpeg's overlay and puts it back: Intel's
	// i965, and AMD without that import.
	OverlayCPU Overlay = "cpu"
)

// ProbeOverlay is how the VAAPI device at device lays a subtitle over the picture, learnt as
// Jellyfin's is from what FFmpeg says opening it: the driver's name, and for Mesa's the extensions
// Vulkan opens the same device with, which map frames between the two, on a kernel with AMD's
// format modifiers.
func ProbeOverlay(ctx context.Context, ffmpeg, device string) Overlay {
	vaapi := said(ctx, ffmpeg, "-init_hw_device", "vaapi=va:"+device)
	var vulkan, kernel string
	if strings.Contains(vaapi, mesaDriver) {
		vulkan = said(ctx, ffmpeg, "-init_hw_device", "drm=dr:"+device, "-init_hw_device", "vulkan=vk@dr")
		// A kernel not read is taken as too old.
		if release, err := os.ReadFile("/proc/sys/kernel/osrelease"); err == nil {
			kernel = string(release)
		}
	}
	return overlayOf(vaapi, vulkan, kernel)
}

const (
	ihdDriver  = "VAAPI driver: Intel iHD driver"
	mesaDriver = "VAAPI driver: Mesa Gallium driver"
)

// vulkanInterop are the Vulkan device extensions Jellyfin's AMD pipeline needs: a VAAPI frame
// imported as a DMA-BUF with its format modifier, and synchronised by a semaphore.
var vulkanInterop = []string{
	"VK_KHR_external_memory_fd", "VK_EXT_external_memory_dma_buf", "VK_KHR_external_semaphore_fd",
	"VK_EXT_external_memory_host", "VK_EXT_image_drm_format_modifier",
}

// overlayOf is the overlay of a device whose opening by FFmpeg said vaapi, by Vulkan said vulkan,
// on the kernel release kernel. Jellyfin's AMD pipeline asks for Linux 5.15, where amdgpu gives
// its frames format modifiers.
func overlayOf(vaapi, vulkan, kernel string) Overlay {
	switch {
	case strings.Contains(vaapi, ihdDriver):
		return OverlayVAAPI
	case strings.Contains(vaapi, mesaDriver):
		for _, ext := range vulkanInterop {
			if !strings.Contains(vulkan, "Using device extension "+ext+"\n") {
				return OverlayCPU
			}
		}
		var major, minor int
		if _, err := fmt.Sscanf(kernel, "%d.%d", &major, &minor); err != nil || major < 5 || major == 5 && minor < 15 {
			return OverlayCPU
		}
		return OverlayVulkan
	}
	return OverlayCPU
}

// said is what FFmpeg says, verbosely, encoding a frame of nothing after args; nothing where it
// fails, as one that cannot open a device does.
func said(ctx context.Context, ffmpeg string, args ...string) string {
	ctx, cancel := media.Within(ctx, ffmpeg, media.PartRun)
	defer cancel()
	a := append([]string{"-v", "verbose", "-hide_banner", "-nostdin"}, args...)
	a = append(a, "-f", "lavfi", "-i", "nullsrc", "-frames:v", "1", "-f", "null", "-")
	var out strings.Builder
	cmd := media.NewCommand(ctx, media.Foreground, ffmpeg, a...)
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return ""
	}
	return out.String()
}

// overlay is how a subtitle drawn on the CPU is laid over the picture on the device, as Jellyfin's
// is: the format it is drawn in, the filter uploading it, and the overlay. None where FFmpeg lays it
// over: software and AMF, whose pictures are on the CPU, and VAAPI on a driver Jellyfin overlays
// on neither the device nor Vulkan with.
func (h Hardware) overlay() (format, upload, overlay string) {
	const pass = "=eof_action=pass:repeatlast=0"
	switch h.Accel {
	case domain.AccelSoftware, domain.AccelAMF:
	case domain.AccelVAAPI:
		switch h.Overlay {
		case OverlayVAAPI:
			return "bgra", "hwupload", "overlay_vaapi" + pass
		case OverlayVulkan, OverlayCPU:
		}
	case domain.AccelVideoToolbox:
		return "bgra", "hwupload", "overlay_videotoolbox" + pass
	case domain.AccelQSV:
		// QSV's frame pool cannot grow.
		return "bgra", "hwupload=extra_hw_frames=64", "overlay_qsv" + pass
	case domain.AccelNVENC:
		// overlay_cuda lays no RGB over YUV.
		return "yuva420p", "hwupload", "overlay_cuda" + pass
	}
	return "", "", ""
}

// longGOP stops an encoder adding keyframes of its own between those it is told to make.
const longGOP = "1000"

// decodes reports whether a source codec is decoded on the device too. H.264 and HEVC are, on
// every device that encodes H.264, but for an interlaced picture on VideoToolbox, which refuses
// one; anything else is decoded by FFmpeg and uploaded.
func (h Hardware) decodes(codec string, e domain.VideoEncode) bool {
	return h.Accel != domain.AccelSoftware && h.Accel != domain.AccelAMF && (codec == "h264" || codec == "hevc") &&
		(!e.Deinterlace || h.Accel != domain.AccelVideoToolbox)
}

// inputArgs opens the device, and decodes on it where it can, before the input.
func (h Hardware) inputArgs(codec string, e domain.VideoEncode) []string {
	var init, decode []string
	switch h.Accel {
	case domain.AccelSoftware, domain.AccelAMF:
		return nil
	case domain.AccelVideoToolbox:
		init = []string{"-init_hw_device", "videotoolbox=hw", "-filter_hw_device", "hw"}
		decode = []string{"-hwaccel", "videotoolbox", "-hwaccel_output_format", "videotoolbox_vld"}
	case domain.AccelVAAPI:
		init = []string{"-init_hw_device", "vaapi=hw:" + h.Device, "-filter_hw_device", "hw"}
		decode = []string{"-hwaccel", "vaapi", "-hwaccel_output_format", "vaapi", "-hwaccel_device", "hw"}
	case domain.AccelQSV:
		// QSV decodes through VAAPI on Linux and D3D11 on Windows, as Jellyfin's does, and its frames
		// are mapped across.
		init = []string{"-init_hw_device", "vaapi=va:" + h.Device, "-init_hw_device", "qsv=hw@va", "-filter_hw_device", "hw"}
		decode = []string{"-hwaccel", "vaapi", "-hwaccel_output_format", "vaapi", "-hwaccel_device", "va"}
		if runtime.GOOS == "windows" {
			init = []string{"-init_hw_device", "d3d11va=va:,vendor=0x8086", "-init_hw_device", "qsv=hw@va", "-filter_hw_device", "hw"}
			decode = []string{"-hwaccel", "d3d11va", "-hwaccel_output_format", "d3d11", "-hwaccel_device", "va"}
		}
	case domain.AccelNVENC:
		init = []string{"-init_hw_device", "cuda=hw:" + h.Device, "-filter_hw_device", "hw"}
		decode = []string{"-hwaccel", "cuda", "-hwaccel_output_format", "cuda", "-hwaccel_device", "hw"}
	}
	if h.decodes(codec, e) {
		return append(init, decode...)
	}
	return init
}

// x265 is Jellyfin's libx265 tuning: no scene cuts or open GOPs, so a forced keyframe starts every
// segment, and its search, lookahead and block sizes trimmed. With it at Jellyfin's veryfast preset
// 10-bit 1080p came to 23 frames a second on four threads of an M5, short of real time; at
// superfast, 54, twice real time with room for a slower processor.
const x265 = "no-scenecut=1:no-open-gop=1:no-info=1:subme=3:merange=25:rc-lookahead=10:me=star:ctu=32:" +
	"max-tu-size=32:min-cu-size=16:rskip=2:rskip-edge-threshold=2:no-sao=1:no-strong-intra-smoothing=1"

// encoder is FFmpeg's encoder of a codec on the device.
func (h Hardware) encoder(codec domain.VideoCodec) string {
	if h.Accel == domain.AccelSoftware {
		return map[domain.VideoCodec]string{domain.VideoH264: "libx264", domain.VideoHEVC: "libx265"}[codec]
	}
	return string(codec) + "_" + string(h.Accel)
}

// videoArgs encodes video on the device, deinterlaced, scaled and tone mapped there, with a
// keyframe where it starts and every SegmentLength after: ffmpeg counts t from the seek. HDR kept
// is encoded in 10 bits, its colours and mastering metadata carried from the frames. It answers
// the filter the picture goes through and the encoder's options.
func (h Hardware) videoArgs(e domain.VideoEncode, codec string) (string, []string) {
	w, ht := strconv.Itoa(e.Width), strconv.Itoa(e.Height)
	kbps := strconv.Itoa(e.BitrateKbps) + "k"
	rate := []string{"-b:v", kbps, "-maxrate", kbps, "-bufsize", strconv.Itoa(2*e.BitrateKbps) + "k"}
	keyframes := []string{"-force_key_frames", "expr:gte(t,n_forced*" + strconv.Itoa(int(SegmentLength.Seconds())) + ")"}
	// The device's pictures are 8 bits, or 10 for HDR kept.
	format, profile := "nv12", "high"
	ten := e.Range == domain.RangeHDR10 || e.Range == domain.RangeHLG
	switch {
	case ten:
		format, profile = "p010", "main10"
	case e.Codec == domain.VideoHEVC:
		profile = "main"
	}
	// Frames decoded by FFmpeg are uploaded to the device first.
	upload := ""
	if !h.decodes(codec, e) {
		upload = "format=nv12|p010le,hwupload,"
	}
	// Deinterlaced first, as Jellyfin's is: with its default, yadif, a frame for each frame, on the
	// device where it has one; QSV's is vpp_qsv's own, below. VideoToolbox's yadif_videotoolbox
	// crashes FFmpeg 9.0.1 (measured on an M-series Mac), so there it is done before the upload.
	deinterlace := ""
	if e.Deinterlace {
		deinterlace = map[domain.Acceleration]string{
			domain.AccelSoftware: "yadif=0:-1:0,", domain.AccelVideoToolbox: "yadif=0:-1:0,", domain.AccelAMF: "yadif=0:-1:0,",
			domain.AccelVAAPI: "deinterlace_vaapi=rate=frame,", domain.AccelNVENC: "yadif_cuda=0:-1:0,",
		}[h.Accel]
	}
	var filter string
	encoder := []string{"-c:v", h.encoder(e.Codec)}
	if e.Codec == domain.VideoHEVC {
		// Apple's players take HEVC only as hvc1.
		encoder = append(encoder, "-tag:v", "hvc1")
	}
	switch h.Accel {
	case domain.AccelSoftware, domain.AccelAMF:
		filter = deinterlace + "scale=" + w + ":" + ht + ",format=yuv420p"
		if ten {
			filter += "10le"
		}
		if e.ToneMap {
			filter = deinterlace + "scale=" + w + ":" + ht + "," + media.ToneMap
		}
		if h.Accel == domain.AccelAMF {
			// ponytail: AMF is given frames decoded, scaled and tone mapped on the CPU, Jellyfin's
			// pipeline where OpenCL is missing; its D3D11 and OpenCL one if the CPU cannot keep up.
			// Jellyfin's rate control, with the qmin of AMF's transcoding preset lowered.
			encoder = append(encoder, "-quality", "speed", "-rc", "cbr", "-qmin", "0", "-qmax", "32")
			if e.Codec == domain.VideoHEVC {
				encoder = append(encoder, "-header_insertion_mode", "gop", "-gops_per_idr", "1")
			}
			break
		}
		// Jellyfin's software transcode: constant quality at its defaults, capped.
		encoder = append(encoder, "-profile:v", profile, "-maxrate", kbps, "-bufsize", strconv.Itoa(2*e.BitrateKbps)+"k")
		switch e.Codec {
		case domain.VideoH264:
			encoder = append(encoder, "-preset", "veryfast", "-crf", "23",
				"-x264opts", "subme=0:me_range=16:rc_lookahead=10:me=hex:open_gop=0", "-sc_threshold", "0")
		case domain.VideoHEVC:
			// x265 would carry a Dolby Vision source's RPU into what it writes as HDR10.
			encoder = append(encoder, "-preset", "superfast", "-crf", "28", "-dolbyvision", "0", "-x265-params", x265)
		}
		return filter, append(encoder, keyframes...)
	case domain.AccelVideoToolbox:
		filter = deinterlace + upload + "scale_vt=w=" + w + ":h=" + ht + ":format=" + format
		if e.ToneMap {
			filter = deinterlace + upload + "scale_vt=w=" + w + ":h=" + ht + ",tonemap_videotoolbox=tonemap=bt2390:t=bt709:m=bt709:p=bt709:format=nv12"
		}
		encoder = append(encoder, "-prio_speed", "1")
	case domain.AccelVAAPI:
		filter = upload + deinterlace + "scale_vaapi=w=" + w + ":h=" + ht + ":format=" + format
		if e.ToneMap {
			filter = upload + deinterlace + "tonemap_vaapi=format=nv12:p=bt709:t=bt709:m=bt709,scale_vaapi=w=" + w + ":h=" + ht
		}
		encoder = append(encoder, "-rc_mode", "VBR")
	case domain.AccelQSV:
		if h.decodes(codec, e) {
			upload = "hwmap=derive_device=qsv,"
		} else {
			upload = "format=nv12|p010le,hwupload=extra_hw_frames=64,"
		}
		filter = upload + "vpp_qsv=w=" + w + ":h=" + ht + ":format=" + format
		if runtime.GOOS == "windows" && h.decodes(codec, e) {
			// D3D11's frame pool cannot grow, so the encoder must not hold the decoder's frames.
			filter += ":passthrough=0"
		}
		if e.Deinterlace {
			filter += ":deinterlace=2"
		}
		if e.ToneMap {
			filter += ":tonemap=1"
		}
		encoder = append(encoder, "-preset", "veryfast", "-forced_idr", "1")
	case domain.AccelNVENC:
		filter = upload + deinterlace + "scale_cuda=w=" + w + ":h=" + ht + ":format=" + format
		if e.ToneMap {
			filter = upload + deinterlace + "scale_cuda=w=" + w + ":h=" + ht + ",tonemap_cuda=format=nv12:p=bt709:t=bt709:m=bt709:tonemap=bt2390:peak=100:desat=0"
		}
		encoder = append(encoder, "-preset", "p1", "-forced-idr", "1")
	}
	encoder = append(encoder, "-profile:v", profile, "-g", longGOP)
	encoder = append(encoder, rate...)
	return filter, append(encoder, keyframes...)
}

// errHardware is a device that would not encode.
var errHardware = errors.New("hls: the hardware would not encode")

// Check encodes a second of SDR test picture, deinterlaced, and of HDR through the device's whole
// chain to codec, as a title would be: tone mapped, and for HEVC kept in 10 bits too. A device that
// is missing, a driver without a filter, or one that cannot encode the codec is found at start
// rather than at the first play.
func (h Hardware) Check(ctx context.Context, ffmpeg string, codec domain.VideoCodec) error {
	const sdr, hdr = "testsrc2=size=1280x720:rate=24,format=yuv420p",
		"testsrc2=size=1280x720:rate=24,format=yuv420p10le,setparams=color_primaries=bt2020:color_trc=smpte2084:colorspace=bt2020nc"
	type test struct {
		source string
		e      domain.VideoEncode
	}
	cases := []test{
		{sdr, domain.VideoEncode{Range: domain.RangeSDR, Deinterlace: true}},
		{hdr, domain.VideoEncode{Range: domain.RangeSDR, ToneMap: true}},
	}
	if codec == domain.VideoHEVC {
		cases = append(cases, test{hdr, domain.VideoEncode{Range: domain.RangeHDR10}})
	}
	for _, tc := range cases {
		// The test picture is decoded by FFmpeg, so no hardware decode is asked for.
		e := tc.e
		e.Codec, e.Width, e.Height, e.BitrateKbps = codec, 640, 360, 2000
		a := []string{"-hide_banner", "-loglevel", "error", "-nostdin"}
		a = append(a, h.inputArgs("rawvideo", e)...)
		a = append(a, "-f", "lavfi", "-i", tc.source, "-t", "1")
		filter, encoder := h.videoArgs(e, "rawvideo")
		a = append(append(a, "-vf", filter), encoder...)
		a = append(a, "-f", "null", "-")
		if err := encodeTest(ctx, ffmpeg, a); err != nil {
			return fmt.Errorf("%w: %s %s (%s, tone mapped %t): %w", errHardware, h.Accel, codec, e.Range, e.ToneMap, err)
		}
	}
	return nil
}

// encodeTest runs one of Check's encodes, as long as a tool reading part of a file may: a driver
// that hangs is a device that would not encode.
func encodeTest(ctx context.Context, ffmpeg string, args []string) error {
	ctx, cancel := media.Within(ctx, ffmpeg, media.PartRun)
	defer cancel()
	cmd := media.NewCommand(ctx, media.Foreground, ffmpeg, args...)
	return cmd.Err(cmd.Run())
}
