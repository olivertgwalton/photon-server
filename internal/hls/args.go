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
// drawn in where there is a layer of it. Each of the subtitle streams is written to descriptors 4
// onwards, a cue at a time, on the file's clock: as SubRip, whose muxer ends a cue as it writes
// it, where WebVTT's ends one only as it begins the next.
func args(in media.Input, hw Hardware, start time.Duration, video domain.VideoPlan, audio *domain.AudioPlan, layer *styledLayer, f domain.SegmentFormat, subtitles []int) []string {
	a := quiet()
	hw = hw.encoding(video)
	if video.Encode != nil {
		a = append(a, hw.inputArgs(video.Codec, *video.Encode)...)
	}
	a = append(a, "-ss", strconv.FormatFloat(start.Seconds(), 'f', 6, 64), "-copyts")
	a = append(a, in.Args()...)
	a = append(a, streamArgs(hw, video, audio, layer)...)
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
	for i, n := range subtitles {
		a = append(a, "-map", "0:"+strconv.Itoa(n), "-c:s", "subrip", "-f", "srt", "-flush_packets", "1", "pipe:"+strconv.Itoa(4+i))
	}
	return a
}

// streamArgs maps the input's video and audio, each copied or encoded as planned, video on hw.
func streamArgs(hw Hardware, video domain.VideoPlan, audio *domain.AudioPlan, layer *styledLayer) []string {
	var a []string
	in := "0:" + strconv.Itoa(video.Stream)
	switch e := video.Encode; {
	case e != nil && layer != nil:
		// Drawn by libass after the picture is scaled and tone mapped, as Jellyfin's is, at the
		// size encoded, so it is as sharp as it can be.
		filter, encoder := hw.videoArgs(*e, video.Codec)
		a = append(append(a, "-map", in, "-vf", filter+","+layer.filter()), encoder...)
	case e != nil && e.Burn != nil:
		// As Jellyfin's: picture and subtitle each scaled to the size encoded, the picture tone
		// mapped first, so the subtitle is drawn as it was authored.
		filter, encoder := hw.videoArgs(*e, video.Codec)
		size := strconv.Itoa(e.Width) + ":" + strconv.Itoa(e.Height)
		format := "yuv420p"
		if e.Range == domain.RangeHDR10 || e.Range == domain.RangeHLG {
			format = "yuv420p10le"
		}
		graph := "[" + in + "]" + filter + "[main];[0:" + strconv.Itoa(*e.Burn) + "]scale=" + size + "[sub];" +
			"[main][sub]overlay=eof_action=pass:repeatlast=0,format=" + format + "[v]"
		a = append(append(a, "-filter_complex", graph, "-map", "[v]"), encoder...)
	case e != nil:
		filter, encoder := hw.videoArgs(*e, video.Codec)
		a = append(append(a, "-map", in, "-vf", filter), encoder...)
	// Apple's players take HEVC only as hvc1, and Dolby Vision as dvh1.
	case video.Codec == "hevc" && video.DolbyVision == domain.DolbyVisionKeep:
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
