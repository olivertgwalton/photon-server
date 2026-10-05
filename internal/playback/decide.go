package playback

import (
	"errors"
	"slices"
	"strings"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/media"
)

// Profile is what a client says it plays, sent with every play as Jellyfin's PlaybackInfo carries
// a device profile. Anything it does not list it cannot play.
type Profile struct {
	// Containers are the FFmpeg demuxer names of the files it opens as they are: "matroska",
	// "mp4", "mpegts".
	Containers []string       `json:"containers"`
	Video      []VideoSupport `json:"video"`
	Audio      []AudioSupport `json:"audio"`
	// MaxBitrateKbps is the most it will be sent, zero for no limit.
	MaxBitrateKbps int `json:"max_bitrate_kbps"`
}

// VideoSupport is a video codec a client decodes, by FFmpeg's name, and how far. A zero limit is
// none.
type VideoSupport struct {
	Codec string `json:"codec"`
	// Profiles are ffprobe's names ("High", "Main 10"); empty is every profile.
	Profiles []string `json:"profiles,omitzero"`
	// MaxLevel is in ffprobe's units: 41 is H.264 level 4.1, 153 is HEVC level 5.1.
	MaxLevel    int `json:"max_level,omitzero"`
	MaxWidth    int `json:"max_width,omitzero"`
	MaxHeight   int `json:"max_height,omitzero"`
	MaxBitDepth int `json:"max_bit_depth,omitzero"`
	// Ranges are the dynamic ranges it shows; empty is SDR alone.
	Ranges []domain.Range `json:"ranges,omitzero"`
	// DolbyVisionProfiles narrows RangeDV to these profiles; empty is every one.
	DolbyVisionProfiles []int `json:"dolby_vision_profiles,omitzero"`
}

// AudioSupport is an audio codec a client decodes, by FFmpeg's name.
type AudioSupport struct {
	Codec       string `json:"codec"`
	MaxChannels int    `json:"max_channels,omitzero"`
}

// Reason is why a copy cannot reach a client as it is, in Jellyfin's TranscodeReason terms.
type Reason string

const (
	ContainerNotSupported       Reason = "container_not_supported"
	VideoCodecNotSupported      Reason = "video_codec_not_supported"
	VideoProfileNotSupported    Reason = "video_profile_not_supported"
	VideoLevelNotSupported      Reason = "video_level_not_supported"
	VideoResolutionNotSupported Reason = "video_resolution_not_supported"
	VideoBitDepthNotSupported   Reason = "video_bit_depth_not_supported"
	VideoRangeNotSupported      Reason = "video_range_not_supported"
	AudioCodecNotSupported      Reason = "audio_codec_not_supported"
	AudioChannelsNotSupported   Reason = "audio_channels_not_supported"
	BitrateExceedsLimit         Reason = "bitrate_exceeds_limit"
)

var (
	// ErrNoCompatibleStream is a copy that cannot be made into anything the client plays.
	ErrNoCompatibleStream = errors.New("playback: nothing the client plays can be made of this copy")
	// ErrNoSuchAudio is an audio stream asked for that the copy does not have.
	ErrNoSuchAudio = errors.New("playback: the copy has no such audio stream")
)

// Copy is what deciding needs of a copy: its container, its bitrate and its first part's streams,
// the parts of one copy being cut from one master.
type Copy struct {
	Container   string
	BitrateKbps int
	Streams     []media.Stream
}

// Decision is how a copy reaches a client: its file as it is, its video copied into HLS, or its
// video encoded again, and why it could not go as it is.
type Decision struct {
	Method  domain.PlayMethod
	Video   *domain.VideoPlan
	Audio   *domain.AudioPlan
	Reasons []Reason
}

// fragmentable are the codecs fragmented MP4 carries, which HLS segments are.
var (
	fragmentableVideo = []string{"h264", "hevc", "av1", "vp9"}
	fragmentableAudio = []string{"aac", "ac3", "eac3", "flac", "opus", "alac", "mp3", "dts", "truehd"}
)

// Decide chooses how a copy plays on a client, with its audio stream as asked, else its default
// one, else its first. It plays as it is where the client opens the container and plays every
// stream; else in HLS with its video copied where the client plays that, encoding the audio where
// it does not; else ErrNoCompatibleStream until video is encoded, with the reasons it could not.
func Decide(p Profile, c Copy, audio *int) (Decision, error) {
	video, sound := pick(c.Streams, audio)
	if audio != nil && sound == nil {
		return Decision{}, ErrNoSuchAudio
	}
	var d Decision
	var videoReasons, audioReasons []Reason
	if video != nil {
		d.Video = &domain.VideoPlan{Stream: video.Index, Codec: video.Codec}
		videoReasons = p.videoReasons(*video)
		if video.DolbyVision != nil && len(videoReasons) == 0 {
			d.Video.DolbyVision = domain.DolbyVisionStrip
			if p.showsDolbyVision(*video) {
				d.Video.DolbyVision = domain.DolbyVisionKeep
			}
		}
	}
	if sound != nil {
		d.Audio = &domain.AudioPlan{Stream: sound.Index}
		audioReasons = p.audioReasons(*sound)
	}
	if !p.opens(c.Container) {
		d.Reasons = append(d.Reasons, ContainerNotSupported)
	}
	d.Reasons = append(d.Reasons, videoReasons...)
	d.Reasons = append(d.Reasons, audioReasons...)
	if p.MaxBitrateKbps > 0 && c.BitrateKbps > p.MaxBitrateKbps {
		d.Reasons = append(d.Reasons, BitrateExceedsLimit)
	}
	switch {
	case len(d.Reasons) == 0:
		d.Method = domain.PlayDirect
		return d, nil
	case video != nil && len(videoReasons) == 0 && !slices.Contains(d.Reasons, BitrateExceedsLimit) &&
		slices.Contains(fragmentableVideo, video.Codec):
		d.Method = domain.PlayRemux
		if sound != nil && (len(audioReasons) > 0 || !slices.Contains(fragmentableAudio, sound.Codec)) {
			enc, ok := p.audioEncode(*sound)
			if !ok {
				return Decision{Reasons: d.Reasons}, ErrNoCompatibleStream
			}
			d.Audio.Encode = &enc
		}
		return d, nil
	}
	return Decision{Reasons: d.Reasons}, ErrNoCompatibleStream
}

// pick finds the first video stream and the audio stream to play.
func pick(streams []media.Stream, audio *int) (video, sound *media.Stream) {
	for i := range streams {
		s := &streams[i]
		switch s.Kind {
		case domain.StreamVideo:
			if video == nil {
				video = s
			}
		case domain.StreamAudio:
			switch {
			case audio != nil:
				if s.Index == *audio {
					sound = s
				}
			case sound == nil, s.Default && !sound.Default:
				sound = s
			}
		case domain.StreamSubtitle:
		}
	}
	return video, sound
}

// opens reports whether the client opens a file of this container, which ffprobe names as every
// demuxer that reads it ("mov,mp4,m4a,3gp,3g2,mj2").
func (p Profile) opens(container string) bool {
	for name := range strings.SplitSeq(container, ",") {
		if slices.Contains(p.Containers, name) {
			return true
		}
	}
	return false
}

func (p Profile) videoReasons(s media.Stream) []Reason {
	i := slices.IndexFunc(p.Video, func(v VideoSupport) bool { return v.Codec == s.Codec })
	if i < 0 {
		return []Reason{VideoCodecNotSupported}
	}
	v := p.Video[i]
	var r []Reason
	if len(v.Profiles) > 0 && s.Profile != "" &&
		!slices.ContainsFunc(v.Profiles, func(name string) bool { return strings.EqualFold(name, s.Profile) }) {
		r = append(r, VideoProfileNotSupported)
	}
	if v.MaxLevel > 0 && s.Level > v.MaxLevel {
		r = append(r, VideoLevelNotSupported)
	}
	if (v.MaxWidth > 0 && s.Width > v.MaxWidth) || (v.MaxHeight > 0 && s.Height > v.MaxHeight) {
		r = append(r, VideoResolutionNotSupported)
	}
	if v.MaxBitDepth > 0 && s.BitDepth > v.MaxBitDepth {
		r = append(r, VideoBitDepthNotSupported)
	}
	if !v.shows(s) {
		r = append(r, VideoRangeNotSupported)
	}
	return r
}

// shows reports whether the client shows a stream's range: HDR10+ falls back to its HDR10, and a
// Dolby Vision profile it does not take to the base layer it is compatible with.
func (v VideoSupport) shows(s media.Stream) bool {
	ranges := v.Ranges
	if len(ranges) == 0 {
		ranges = []domain.Range{domain.RangeSDR}
	}
	r := s.Range
	if r == "" {
		r = domain.RangeSDR
	}
	if slices.Contains(ranges, r) {
		return r != domain.RangeDV || s.DolbyVision == nil || len(v.DolbyVisionProfiles) == 0 ||
			slices.Contains(v.DolbyVisionProfiles, s.DolbyVision.Profile) || v.showsBase(s.DolbyVision)
	}
	switch r {
	case domain.RangeHDR10Plus:
		return slices.Contains(ranges, domain.RangeHDR10)
	case domain.RangeDV:
		return s.DolbyVision != nil && v.showsBase(s.DolbyVision)
	case domain.RangeSDR, domain.RangeHLG, domain.RangeHDR10:
	}
	return false
}

// showsDolbyVision reports whether the client shows a stream's Dolby Vision itself, not only its
// base layer.
func (p Profile) showsDolbyVision(s media.Stream) bool {
	i := slices.IndexFunc(p.Video, func(v VideoSupport) bool { return v.Codec == s.Codec })
	return i >= 0 && slices.Contains(p.Video[i].Ranges, domain.RangeDV) &&
		(len(p.Video[i].DolbyVisionProfiles) == 0 || slices.Contains(p.Video[i].DolbyVisionProfiles, s.DolbyVision.Profile))
}

// showsBase reports whether the client shows a Dolby Vision stream's base layer by itself.
func (v VideoSupport) showsBase(dv *media.DolbyVision) bool {
	ranges := v.Ranges
	if len(ranges) == 0 {
		ranges = []domain.Range{domain.RangeSDR}
	}
	switch dv.Compatibility {
	case 1:
		return slices.Contains(ranges, domain.RangeHDR10)
	case 2:
		return slices.Contains(ranges, domain.RangeSDR)
	case 4:
		return slices.Contains(ranges, domain.RangeHLG)
	}
	return false
}

func (p Profile) audioReasons(s media.Stream) []Reason {
	a, ok := p.audio(s.Codec)
	switch {
	case !ok:
		return []Reason{AudioCodecNotSupported}
	case a.MaxChannels > 0 && s.Channels > a.MaxChannels:
		return []Reason{AudioChannelsNotSupported}
	}
	return nil
}

func (p Profile) audio(codec string) (AudioSupport, bool) {
	i := slices.IndexFunc(p.Audio, func(a AudioSupport) bool { return a.Codec == codec })
	if i < 0 {
		return AudioSupport{}, false
	}
	return p.Audio[i], true
}

// encoders are the audio codecs the server encodes to, with the most channels FFmpeg's encoder
// writes.
var encoders = map[string]int{"aac": 8, "eac3": 6, "ac3": 6}

// audioEncode chooses what a stream the client cannot take is encoded to: the first codec in the
// client's list the server encodes, as Jellyfin takes a transcoding profile's codecs in order,
// keeping as many of its channels as both allow, at Jellyfin's bitrates.
func (p Profile) audioEncode(s media.Stream) (domain.AudioEncode, bool) {
	for _, a := range p.Audio {
		most, ok := encoders[a.Codec]
		if !ok {
			continue
		}
		if a.MaxChannels > 0 {
			most = min(most, a.MaxChannels)
		}
		channels := min(max(s.Channels, 1), most)
		// Only layouts players know, as Jellyfin's HLS does: 5 channels go up to 5.1 or down to
		// stereo, 7 up to 7.1 or down to 5.1, and 3 or 4 to stereo.
		switch {
		case channels == 3 || channels == 4 || (channels == 5 && most < 6):
			channels = 2
		case channels == 5, channels == 7 && most < 8:
			channels = 6
		case channels == 7:
			channels = 8
		}
		kbps := 128 * channels
		if channels >= 6 {
			kbps = 640
		}
		return domain.AudioEncode{Codec: a.Codec, Channels: channels, BitrateKbps: kbps}, true
	}
	return domain.AudioEncode{}, false
}
