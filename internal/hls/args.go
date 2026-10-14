package hls

import (
	"strconv"
	"time"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/media"
)

// quiet starts an ffmpeg run that says only what went wrong and reads nothing from stdin.
func quiet() []string {
	return []string{"-hide_banner", "-loglevel", "error", "-nostdin"}
}

// args copies or encodes a file's video and its audio into fragmented MP4 or MPEG-TS on stdout,
// from start, on the file's own clock (see clockOffset). Copied video starts at the keyframe at
// start; encoded video makes one there and every SegmentLength after, on hw, with styled text
// drawn in where there is a layer of it. Each of the subtitle streams is written to its socket,
// a cue at a time, on the file's clock: as SubRip, whose muxer ends a cue as it writes
// it, where WebVTT's ends one only as it begins the next.
func args(in media.Input, hw Hardware, start time.Duration, video domain.VideoPlan, audio *domain.AudioPlan, layer *styledLayer, f domain.SegmentFormat, subtitles []subtitleSocket) []string {
	a := quiet()
	if video.Encode != nil {
		a = append(a, hw.inputArgs(video.Codec, *video.Encode)...)
	} else {
		// Copied video starts at the keyframe before start, and audio encoded beside it must too.
		// Trimmed to start, it has nothing yet at the next keyframe, and the MP4 muxer, holding
		// its first fragment until every track has a sample (delay_moov), puts that keyframe in
		// the fragment cut drops as before the segment.
		// AVI gives a stream with B-frames no packet a PTS, which MPEG-TS refuses and MP4 shows in
		// decode order; genpts makes them from the DTS, as Jellyfin's copies and the keyframe walk do.
		a = append(a, "-noaccurate_seek", "-fflags", "+genpts")
	}
	if start > 0 {
		// Only past the start, as Jellyfin's: ffmpeg moves a seek in a stream with B-frames 3/23 s
		// earlier, AVI refuses one before its start, and the refusal drops the first keyframe's GOP.
		a = append(a, "-ss", strconv.FormatFloat(start.Seconds(), 'f', 6, 64))
	}
	a = append(a, "-copyts")
	a = append(a, in.Args()...)
	a = append(a, streamArgs(hw, start, video, audio, layer)...)
	// The MP4 muxer writes a file's chapters as a text track, which Apple's players refuse a
	// segment for.
	a = append(a, "-map_chapters", "-1")
	switch f {
	case domain.SegmentsMPEGTS:
		// Without mpegts_copyts the muxer moves every timestamp on by its delay.
		a = append(a, "-f", "mpegts", "-mpegts_copyts", "1")
	case domain.SegmentsFMP4:
		// Dolby Vision's configuration, TrueHD and DTS are experimental in FFmpeg's MP4 muxer.
		a = append(a,
			"-strict", "experimental",
			"-f", "mp4", "-movflags", "+frag_keyframe+empty_moov+default_base_moof+delay_moov+frag_discont+skip_trailer",
			"-use_editlist", "0",
		)
	}
	a = append(a,
		"-avoid_negative_ts", "disabled",
		"-output_ts_offset", strconv.FormatFloat(clockOffset.Seconds(), 'f', 0, 64),
		"-fflags", "+bitexact", "-",
	)
	for _, s := range subtitles {
		a = append(a, "-map", "0:"+strconv.Itoa(s.stream), "-c:s", "subrip", "-f", "srt", "-flush_packets", "1", s.url())
	}
	return a
}

// burnArgs draws a subtitle into the picture in, which filter scales and tone maps on hw, as
// Jellyfin's does: the subtitle drawn on the CPU at the size encoded, a picture one scaled to it
// and styled text by libass, so it is drawn as it was authored and as sharp as it can be.
func burnArgs(hw Hardware, start time.Duration, in, filter string, e domain.VideoEncode, layer *styledLayer) []string {
	size := strconv.Itoa(e.Width) + ":" + strconv.Itoa(e.Height)
	if format, upload, overlay := hw.overlay(); overlay != "" {
		// Uploaded and laid over the picture on the device; styled text drawn on a clear canvas.
		var sub string
		if layer != nil {
			sub = layer.canvas(e.Width, e.Height, start, format)
			if hw.Accel == domain.AccelNVENC {
				// libass draws onto a YUV canvas premultiplied, which only overlay_cuda is told.
				overlay += ":alpha_format=premultiplied"
			}
		} else {
			sub = "[0:" + strconv.Itoa(*e.Burn) + "]scale=" + size + ",format=" + format
		}
		graph := sub + "," + upload + "[sub];[" + in + "]" + filter + "[main];[main][sub]" + overlay + "[v]"
		return []string{"-filter_complex", graph, "-map", "[v]"}
	}
	// Laid over the picture by FFmpeg, VAAPI's taken off the device and back.
	ten := e.Range == domain.RangeHDR10 || e.Range == domain.RangeHLG
	format, down, up := "yuv420p", "", ""
	if ten {
		format = "yuv420p10le"
	}
	if hw.Accel == domain.AccelVAAPI {
		format = "nv12"
		if ten {
			format = "p010"
		}
		down, up = ",hwdownload,format="+format, ",hwupload"
	}
	if layer != nil {
		return []string{"-map", in, "-vf", filter + down + "," + layer.filter("") + up}
	}
	graph := "[" + in + "]" + filter + down + "[main];[0:" + strconv.Itoa(*e.Burn) + "]scale=" + size + "[sub];" +
		"[main][sub]overlay=eof_action=pass:repeatlast=0,format=" + format + up + "[v]"
	return []string{"-filter_complex", graph, "-map", "[v]"}
}

// streamArgs maps the input's video and audio, each copied or encoded as planned, video on hw,
// from start.
func streamArgs(hw Hardware, start time.Duration, video domain.VideoPlan, audio *domain.AudioPlan, layer *styledLayer) []string {
	var a []string
	in := "0:" + strconv.Itoa(video.Stream)
	switch e := video.Encode; {
	case video.Burns():
		filter, encoder := hw.videoArgs(*e, video.Codec)
		a = append(append(a, burnArgs(hw, start, in, filter, *e, layer)...), encoder...)
	case e != nil:
		filter, encoder := hw.videoArgs(*e, video.Codec)
		a = append(append(a, "-map", in, "-vf", filter), encoder...)
	// Apple's players take HEVC only as hvc1, and Dolby Vision as dvh1.
	case video.Codec == "hevc" && (video.DolbyVision == domain.DolbyVisionKeep || video.DolbyVision == domain.DolbyVisionConvert):
		a = append(a, "-map", in, "-c:v", "copy", "-tag:v", "dvh1")
	case video.Codec == "hevc":
		a = append(a, "-map", in, "-c:v", "copy", "-tag:v", "hvc1")
	default:
		a = append(a, "-map", in, "-c:v", "copy")
	}
	if video.DolbyVision == domain.DolbyVisionStrip {
		a = append(a, "-bsf:v", "dovi_rpu=strip=1")
	}
	if audio != nil {
		a = append(a, "-map", "0:"+strconv.Itoa(audio.Stream))
		if e := audio.Encode; e != nil {
			if e.Boost > 0 {
				a = append(a, "-af", "volume="+strconv.FormatFloat(e.Boost, 'f', -1, 64))
			}
			a = append(a, "-c:a", e.Codec, "-ac", strconv.Itoa(e.Channels), "-b:a", strconv.Itoa(e.BitrateKbps)+"k")
		} else {
			a = append(a, "-c:a", "copy")
		}
	}
	return a
}
