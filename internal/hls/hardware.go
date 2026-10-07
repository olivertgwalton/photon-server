package hls

import (
	"context"
	"errors"
	"fmt"
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
	// HEVC is whether video is encoded to HEVC for a client that plays it.
	HEVC domain.HEVCEncoding
}

// encoding is the hardware video planned so is encoded on: a subtitle is drawn in in software, as
// each device overlays in its own way.
func (h Hardware) encoding(video domain.VideoPlan) Hardware {
	if video.Burns() {
		return Hardware{Accel: domain.AccelSoftware, HEVC: h.HEVC}
	}
	return h
}

// longGOP stops an encoder adding keyframes of its own between those it is told to make.
const longGOP = "1000"

// decodes reports whether a source codec is decoded on the device too. H.264 and HEVC are, on
// every device that encodes H.264, but for an interlaced picture on VideoToolbox, which refuses
// one; anything else is decoded by FFmpeg and uploaded.
func (h Hardware) decodes(codec string, e domain.VideoEncode) bool {
	return h.Accel != domain.AccelSoftware && (codec == "h264" || codec == "hevc") &&
		(!e.Deinterlace || h.Accel != domain.AccelVideoToolbox)
}

// inputArgs opens the device, and decodes on it where it can, before the input.
func (h Hardware) inputArgs(codec string, e domain.VideoEncode) []string {
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
			domain.AccelSoftware: "yadif=0:-1:0,", domain.AccelVideoToolbox: "yadif=0:-1:0,",
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
	case domain.AccelSoftware:
		filter = deinterlace + "scale=" + w + ":" + ht + ",format=yuv420p"
		if ten {
			filter += "10le"
		}
		if e.ToneMap {
			filter = deinterlace + "scale=" + w + ":" + ht + "," + media.ToneMap
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
	cmd := media.NewCommand(ctx, media.Foreground, nil, ffmpeg, args...)
	return cmd.Err(cmd.Run())
}
