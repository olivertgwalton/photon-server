package playback

import (
	"fmt"
	"slices"
	"strings"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/hls"
)

// h264Profiles are H.264's profiles as ffprobe names them, written as RFC 6381's profile and
// constraint bytes.
var h264Profiles = map[string]string{
	"Constrained Baseline": "42E0", "Baseline": "4200", "Main": "4D40", "High": "6400", "High 10": "6E00",
}

// audioCodecs are audio formats as RFC 6381 names them in MP4, as Jellyfin's master playlist does.
var audioCodecs = map[string]string{
	"aac": "mp4a.40.2", "mp3": "mp4a.40.34", "ac3": "ac-3", "eac3": "ec-3", "flac": "fLaC", "alac": "alac",
	"opus": "Opus", "truehd": "mlpa", "dts": "dtsc",
}

// variant describes a copy's HLS variant as Jellyfin's master playlist does: its bandwidth, the
// formats of the video and audio as they are sent, and the video's range, size and frame rate. Codecs are left out
// where any one is not known, since a client takes a list as all there is.
func variant(streams []domain.Stream, video domain.VideoPlan, audio *domain.AudioPlan, kbps int) hls.Variant {
	v := hls.Variant{BandwidthKbps: kbps}
	var picture domain.Stream
	for _, s := range streams {
		if s.Index == video.Stream && s.Kind == domain.StreamVideo {
			picture = s
		}
	}
	codecs := []string{videoCodec(picture, video)}
	if audio != nil {
		codecs = append(codecs, audioCodec(streams, *audio))
	}
	if !slices.Contains(codecs, "") {
		v.Codecs = codecs
	}
	v.Range, v.Width, v.Height, v.FrameRate = videoRange(picture, video), picture.Width, picture.Height, picture.FrameRate
	if e := video.Encode; e != nil {
		v.Width, v.Height = e.Width, e.Height
	}
	return v
}

func videoCodec(s domain.Stream, v domain.VideoPlan) string {
	if e := v.Encode; e != nil {
		// At Jellyfin's level 4.1, or 5.1 for a picture larger than 1080p: H.264's High profile,
		// HEVC's Main, or Main 10 for HDR kept, whose level is thirty times rather than ten.
		level := 41
		if e.Width*e.Height > 1920*1088 {
			level = 51
		}
		switch {
		case e.Codec == domain.VideoH264:
			return fmt.Sprintf("avc1.6400%02X", level)
		case e.Range == domain.RangeHDR10 || e.Range == domain.RangeHLG:
			return fmt.Sprintf("hvc1.2.4.L%d.B0", 3*level)
		}
		return fmt.Sprintf("hvc1.1.6.L%d.B0", 3*level)
	}
	switch {
	case v.DolbyVision == domain.DolbyVisionKeep && s.DolbyVision != nil:
		return fmt.Sprintf("dvh1.%02d.%02d", s.DolbyVision.Profile, s.DolbyVision.Level)
	case s.Codec == "h264" && h264Profiles[s.Profile] != "" && s.Level > 0:
		return fmt.Sprintf("avc1.%s%02X", h264Profiles[s.Profile], s.Level)
	case s.Codec == "hevc" && s.Level > 0:
		// ffprobe's HEVC level is already thirty times the level, as the string wants it.
		profile := "1"
		if s.Profile == "Main 10" {
			profile = "2"
		}
		return fmt.Sprintf("hvc1.%s.4.L%d.B0", profile, s.Level)
	}
	return ""
}

func audioCodec(streams []domain.Stream, a domain.AudioPlan) string {
	if e := a.Encode; e != nil {
		return audioCodecs[e.Codec]
	}
	for _, s := range streams {
		if s.Index != a.Stream {
			continue
		}
		switch {
		case s.Codec == "aac" && s.Profile == "HE-AAC":
			return "mp4a.40.5"
		case s.Codec == "dts" && strings.HasPrefix(s.Profile, "DTS-HD"):
			return "dtsh"
		}
		return audioCodecs[s.Codec]
	}
	return ""
}

// videoRange is the range the video is sent in: the one it is encoded in, else the stream's own,
// Dolby Vision's being its base layer's.
func videoRange(s domain.Stream, v domain.VideoPlan) string {
	r := s.Range
	if v.Encode != nil {
		r = v.Encode.Range
	}
	switch r {
	case domain.RangeSDR:
		return "SDR"
	case domain.RangeHDR10, domain.RangeHDR10Plus:
		return "PQ"
	case domain.RangeHLG:
		return "HLG"
	case domain.RangeDV:
		if dv := s.DolbyVision; dv != nil && dv.Compatibility == 2 {
			return "SDR"
		} else if dv != nil && dv.Compatibility == 4 {
			return "HLG"
		}
		return "PQ"
	}
	return ""
}
