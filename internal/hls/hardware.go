package hls

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/media"
)

// Hardware is the device video is encoded on, chosen for the whole server as Jellyfin's is:
// VideoToolbox on a Mac, VAAPI or QSV on an Intel or AMD render node, NVENC on an NVIDIA card.
type Hardware struct {
	Accel domain.Acceleration
	// Device is the VAAPI or QSV render node, or the CUDA device's index.
	Device string
}

// longGOP stops an encoder adding keyframes of its own between those it is told to make.
const longGOP = "1000"

// decodes reports whether a source codec is decoded on the device too. H.264 and HEVC are, on
// every device that encodes H.264; anything else is decoded by FFmpeg and uploaded.
func (h Hardware) decodes(codec string) bool {
	return h.Accel != domain.AccelSoftware && (codec == "h264" || codec == "hevc")
}

// inputArgs opens the device, and decodes on it where it can, before the input.
func (h Hardware) inputArgs(codec string) []string {
	var init, decode []string
	switch h.Accel {
	case domain.AccelSoftware:
		return nil
	case domain.AccelVideoToolbox:
		init = []string{"-init_hw_device", "videotoolbox=hw", "-filter_hw_device", "hw"}
		decode = []string{"-hwaccel", "videotoolbox", "-hwaccel_output_format", "videotoolbox_vld"}
	case domain.AccelVAAPI:
		init = []string{"-init_hw_device", "vaapi=hw:" + h.Device, "-filter_hw_device", "hw"}
		decode = []string{"-hwaccel", "vaapi", "-hwaccel_output_format", "vaapi", "-hwaccel_device", "hw"}
	case domain.AccelQSV:
		// QSV decodes through VAAPI on Linux, as Jellyfin's does, and its frames are mapped across.
		init = []string{"-init_hw_device", "vaapi=va:" + h.Device, "-init_hw_device", "qsv=hw@va", "-filter_hw_device", "hw"}
		decode = []string{"-hwaccel", "vaapi", "-hwaccel_output_format", "vaapi", "-hwaccel_device", "va"}
	case domain.AccelNVENC:
		init = []string{"-init_hw_device", "cuda=hw:" + h.Device, "-filter_hw_device", "hw"}
		decode = []string{"-hwaccel", "cuda", "-hwaccel_output_format", "cuda", "-hwaccel_device", "hw"}
	}
	if h.decodes(codec) {
		return append(init, decode...)
	}
	return init
}

// videoArgs encodes video to H.264 on the device, scaled and tone mapped there, with a keyframe
// where it starts and every SegmentLength after: ffmpeg counts t from the seek. It answers the
// filter the picture goes through and the encoder's options.
func (h Hardware) videoArgs(e domain.VideoEncode, codec string) (string, []string) {
	w, ht := strconv.Itoa(e.Width), strconv.Itoa(e.Height)
	kbps := strconv.Itoa(e.BitrateKbps) + "k"
	rate := []string{"-b:v", kbps, "-maxrate", kbps, "-bufsize", strconv.Itoa(2*e.BitrateKbps) + "k"}
	keyframes := []string{"-force_key_frames", "expr:gte(t,n_forced*" + strconv.Itoa(int(SegmentLength.Seconds())) + ")"}
	// Frames decoded by FFmpeg are uploaded to the device first.
	upload := ""
	if !h.decodes(codec) {
		upload = "format=nv12|p010le,hwupload,"
	}
	var filter string
	var encoder []string
	switch h.Accel {
	case domain.AccelSoftware:
		filter = "scale=" + w + ":" + ht + ",format=yuv420p"
		if e.ToneMap {
			filter = "scale=" + w + ":" + ht + "," + media.ToneMap
		}
		// Jellyfin's software transcode: x264's veryfast preset at constant quality, capped.
		encoder = []string{
			"-c:v", "libx264", "-preset", "veryfast", "-crf", "23", "-profile:v", "high",
			"-maxrate", kbps, "-bufsize", strconv.Itoa(2*e.BitrateKbps) + "k",
			"-x264opts", "subme=0:me_range=16:rc_lookahead=10:me=hex:open_gop=0", "-sc_threshold", "0",
		}
		return filter, append(encoder, keyframes...)
	case domain.AccelVideoToolbox:
		filter = upload + "scale_vt=w=" + w + ":h=" + ht + ":format=nv12"
		if e.ToneMap {
			filter = upload + "scale_vt=w=" + w + ":h=" + ht + ",tonemap_videotoolbox=tonemap=bt2390:t=bt709:m=bt709:p=bt709:format=nv12"
		}
		encoder = []string{"-c:v", "h264_videotoolbox", "-prio_speed", "1"}
	case domain.AccelVAAPI:
		filter = upload + "scale_vaapi=w=" + w + ":h=" + ht + ":format=nv12"
		if e.ToneMap {
			filter = upload + "tonemap_vaapi=format=nv12:p=bt709:t=bt709:m=bt709,scale_vaapi=w=" + w + ":h=" + ht
		}
		encoder = []string{"-c:v", "h264_vaapi", "-rc_mode", "VBR"}
	case domain.AccelQSV:
		if h.decodes(codec) {
			upload = "hwmap=derive_device=qsv,"
		} else {
			upload = "format=nv12|p010le,hwupload=extra_hw_frames=64,"
		}
		filter = upload + "vpp_qsv=w=" + w + ":h=" + ht + ":format=nv12"
		if e.ToneMap {
			filter += ":tonemap=1"
		}
		encoder = []string{"-c:v", "h264_qsv", "-preset", "veryfast", "-forced_idr", "1"}
	case domain.AccelNVENC:
		filter = upload + "scale_cuda=w=" + w + ":h=" + ht + ":format=nv12"
		if e.ToneMap {
			filter = upload + "scale_cuda=w=" + w + ":h=" + ht + ",tonemap_cuda=format=nv12:p=bt709:t=bt709:m=bt709:tonemap=bt2390:peak=100:desat=0"
		}
		encoder = []string{"-c:v", "h264_nvenc", "-preset", "p1", "-forced-idr", "1"}
	}
	encoder = append(encoder, "-profile:v", "high", "-g", longGOP)
	encoder = append(encoder, rate...)
	return filter, append(encoder, keyframes...)
}

// errHardware is a device that would not encode.
var errHardware = errors.New("hls: the hardware would not encode")

// Check encodes a second of SDR and of HDR test picture through the device's whole chain, as a
// title would be: a device that is missing, or a driver without a filter, is found at start rather
// than at the first play.
func (h Hardware) Check(ctx context.Context, ffmpeg string) error {
	for _, tc := range []struct {
		source string
		hdr    bool
	}{
		{"testsrc2=size=1280x720:rate=24,format=yuv420p", false},
		{"testsrc2=size=1280x720:rate=24,format=yuv420p10le,setparams=color_primaries=bt2020:color_trc=smpte2084:colorspace=bt2020nc", true},
	} {
		// The test picture is decoded by FFmpeg, so no hardware decode is asked for.
		a := []string{"-hide_banner", "-loglevel", "error", "-nostdin"}
		a = append(a, h.inputArgs("rawvideo")...)
		a = append(a, "-f", "lavfi", "-i", tc.source, "-t", "1")
		filter, encoder := h.videoArgs(domain.VideoEncode{Codec: "h264", Width: 640, Height: 360, BitrateKbps: 2000, ToneMap: tc.hdr}, "rawvideo")
		a = append(append(a, "-vf", filter), encoder...)
		a = append(a, "-f", "null", "-")
		cmd := exec.CommandContext(ctx, ffmpeg, a...)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("%w: %s (hdr %t): %w: %s", errHardware, h.Accel, tc.hdr, err, bytes.TrimSpace(stderr.Bytes()))
		}
	}
	return nil
}
