package playback

import (
	"cmp"
	"errors"
	"math"
	"slices"
	"strings"

	"github.com/olivertgwalton/photon-server/internal/domain"
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
	// Subtitles are the subtitle formats it draws itself from a file it plays as it is, by
	// FFmpeg's names ("subrip", "hdmv_pgs_subtitle").
	Subtitles []string `json:"subtitles,omitzero"`
	// Parts is how it plays a copy in several files: joined into one HLS stream by the server, the
	// default, or each file in turn as it is.
	Parts domain.PartPlayback `json:"parts,omitzero"`
	// Segments is the container of the HLS segments it plays: fragmented MP4, the default, or
	// MPEG-TS.
	Segments domain.SegmentFormat `json:"segments,omitzero"`
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

var (
	// ErrNoCompatibleStream is a copy that cannot be made into anything the client plays.
	ErrNoCompatibleStream = errors.New("playback: nothing the client plays can be made of this copy")
	// ErrNoSuchAudio is an audio stream asked for that the copy does not have.
	ErrNoSuchAudio = errors.New("playback: the copy has no such audio stream")
	// ErrNoSuchSubtitle is a subtitle stream asked for that the copy does not have.
	ErrNoSuchSubtitle = errors.New("playback: the copy has no such subtitle stream")
)

// Copy is what deciding needs of a copy: its container, its bitrate, how many files it is in, and
// its first part's streams, the parts of one copy being cut from one master.
type Copy struct {
	Container   string
	BitrateKbps int
	Parts       int
	Streams     []domain.Stream
}

// Decision is how a copy reaches a client: its file as it is, its video copied into HLS, or its
// video encoded again, and why it could not go as it is.
type Decision struct {
	Method  domain.PlayMethod
	Video   *domain.VideoPlan
	Audio   *domain.AudioPlan
	Reasons []domain.TranscodeReason
}

// pictureSubtitles are subtitle codecs that are pictures, which only a client drawing them from the
// file shows, or which are drawn into the video.
var pictureSubtitles = []string{"hdmv_pgs_subtitle", "dvd_subtitle", "dvb_subtitle", "xsub"}

// carried are the codecs HLS segments of a format carry: fragmented MP4 nearly any, MPEG-TS those
// players take from it, which is no Dolby Vision either.
func carried(f domain.SegmentFormat) (video, audio []string) {
	switch f {
	case domain.SegmentsMPEGTS:
		return []string{"h264", "hevc"}, []string{"aac", "ac3", "eac3", "mp3"}
	case domain.SegmentsFMP4:
	}
	return []string{"h264", "hevc", "av1", "vp9"}, []string{"aac", "ac3", "eac3", "flac", "opus", "alac", "mp3", "dts", "truehd"}
}

// Decide chooses how a copy plays on a client, with its audio stream as asked, else its default
// one, else its first. It plays as it is where the client opens the container and plays every
// stream; else in HLS with its video copied where the client plays that, encoding the audio where
// it does not; else in HLS with its video encoded, to HEVC where the client plays it and hevc
// allows it, else to H.264. ErrNoCompatibleStream, with the reasons it could not play as it is,
// where the client takes none of these.
//
// A picture subtitle asked for (PGS, DVD) is drawn into the video where the client cannot draw it
// from the file itself, as HLS carries no pictures: as Jellyfin's subtitle Encode method does.
func Decide(p Profile, c Copy, audio, subtitle *int, hevc domain.HEVCEncoding) (Decision, error) {
	video, sound := pick(c.Streams, audio)
	if audio != nil && sound == nil {
		return Decision{}, ErrNoSuchAudio
	}
	var burn *int
	var burnCodec string
	if subtitle != nil {
		i := slices.IndexFunc(c.Streams, func(s domain.Stream) bool { return s.Kind == domain.StreamSubtitle && s.Index == *subtitle })
		if i < 0 {
			return Decision{}, ErrNoSuchSubtitle
		}
		if slices.Contains(pictureSubtitles, c.Streams[i].Codec) {
			burn, burnCodec = subtitle, c.Streams[i].Codec
		}
	}
	var d Decision
	var videoReasons, audioReasons []domain.TranscodeReason
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
		d.Reasons = append(d.Reasons, domain.ContainerNotSupported)
	}
	if c.Parts > 1 && p.Parts != domain.PartsEach {
		d.Reasons = append(d.Reasons, domain.PartsNotSupported)
	}
	d.Reasons = append(d.Reasons, videoReasons...)
	d.Reasons = append(d.Reasons, audioReasons...)
	tooMuch := p.MaxBitrateKbps > 0 && c.BitrateKbps > p.MaxBitrateKbps
	if tooMuch {
		d.Reasons = append(d.Reasons, domain.BitrateExceedsLimit)
	}
	if burn != nil && !slices.Contains(p.Subtitles, burnCodec) {
		d.Reasons = append(d.Reasons, domain.SubtitleCodecNotSupported)
	}
	if len(d.Reasons) == 0 {
		d.Method = domain.PlayDirect
		return d, nil
	}
	if video == nil {
		return Decision{Reasons: d.Reasons}, ErrNoCompatibleStream
	}
	d.Method = domain.PlayRemux
	carriedVideo, carriedAudio := carried(p.Segments)
	// MPEG-TS has no Dolby Vision: the client is sent the base layer where it shows that alone.
	lostDV := false
	if p.Segments == domain.SegmentsMPEGTS && d.Video.DolbyVision == domain.DolbyVisionKeep {
		if v, _ := p.video(video.Codec); v.showsBase(video.DolbyVision) {
			d.Video.DolbyVision = domain.DolbyVisionStrip
		} else {
			lostDV = true
		}
	}
	// Out of a file played as it is, a picture subtitle reaches the client only drawn in.
	if len(videoReasons) > 0 || tooMuch || burn != nil || lostDV || !slices.Contains(carriedVideo, video.Codec) {
		enc, ok := p.videoEncode(*video, c.BitrateKbps, hevc)
		if !ok {
			return Decision{Reasons: d.Reasons}, ErrNoCompatibleStream
		}
		enc.Burn = burn
		d.Method, d.Video.Encode, d.Video.DolbyVision = domain.PlayTranscode, &enc, domain.DolbyVisionNone
	}
	if sound != nil {
		// Encoded video shares the client's limit with the audio, and audio has a share of it; a
		// copy whose video is copied fits it whole already.
		budget := 0
		if d.Video.Encode != nil {
			budget = p.audioBudget()
		}
		// Under a limit, audio of a bitrate nobody knows may be lossless, and is not risked.
		copied := len(audioReasons) == 0 && slices.Contains(carriedAudio, sound.Codec) &&
			(budget == 0 || (sound.BitrateKbps > 0 && sound.BitrateKbps <= budget))
		if !copied {
			enc, ok := p.audioEncode(*sound)
			if !ok {
				return Decision{Reasons: d.Reasons}, ErrNoCompatibleStream
			}
			if budget > 0 {
				enc.BitrateKbps = min(enc.BitrateKbps, budget)
			}
			d.Audio.Encode = &enc
		}
	}
	if e := d.Video.Encode; e != nil && p.MaxBitrateKbps > 0 {
		e.BitrateKbps = max(min(e.BitrateKbps, p.MaxBitrateKbps-d.audioKbps(sound)), 64)
	}
	return d, nil
}

// audioKbps is what the decided audio spends of the client's bitrate.
func (d Decision) audioKbps(sound *domain.Stream) int {
	switch {
	case sound == nil:
		return 0
	case d.Audio.Encode != nil:
		return d.Audio.Encode.BitrateKbps
	}
	return sound.BitrateKbps
}

// pick finds the first video stream and the audio stream to play.
func pick(streams []domain.Stream, audio *int) (video, sound *domain.Stream) {
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

func (p Profile) videoReasons(s domain.Stream) []domain.TranscodeReason {
	v, ok := p.video(s.Codec)
	if !ok {
		return []domain.TranscodeReason{domain.VideoCodecNotSupported}
	}
	var r []domain.TranscodeReason
	if len(v.Profiles) > 0 && s.Profile != "" &&
		!slices.ContainsFunc(v.Profiles, func(name string) bool { return strings.EqualFold(name, s.Profile) }) {
		r = append(r, domain.VideoProfileNotSupported)
	}
	if v.MaxLevel > 0 && s.Level > v.MaxLevel {
		r = append(r, domain.VideoLevelNotSupported)
	}
	if (v.MaxWidth > 0 && s.Width > v.MaxWidth) || (v.MaxHeight > 0 && s.Height > v.MaxHeight) {
		r = append(r, domain.VideoResolutionNotSupported)
	}
	if v.MaxBitDepth > 0 && s.BitDepth > v.MaxBitDepth {
		r = append(r, domain.VideoBitDepthNotSupported)
	}
	if !v.shows(s) {
		r = append(r, domain.VideoRangeNotSupported)
	}
	return r
}

// shows reports whether the client shows a stream's range: HDR10+ falls back to its HDR10, and a
// Dolby Vision profile it does not take to the base layer it is compatible with.
func (v VideoSupport) shows(s domain.Stream) bool {
	ranges := v.ranges()
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

// ranges are the ranges the client shows, SDR alone where it lists none.
func (v VideoSupport) ranges() []domain.Range {
	if len(v.Ranges) == 0 {
		return []domain.Range{domain.RangeSDR}
	}
	return v.Ranges
}

// showsDolbyVision reports whether the client shows a stream's Dolby Vision itself, not only its
// base layer.
func (p Profile) showsDolbyVision(s domain.Stream) bool {
	v, ok := p.video(s.Codec)
	return ok && slices.Contains(v.Ranges, domain.RangeDV) &&
		(len(v.DolbyVisionProfiles) == 0 || slices.Contains(v.DolbyVisionProfiles, s.DolbyVision.Profile))
}

// showsBase reports whether the client shows a Dolby Vision stream's base layer by itself.
func (v VideoSupport) showsBase(dv *domain.DolbyVision) bool {
	ranges := v.ranges()
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

func (p Profile) audioReasons(s domain.Stream) []domain.TranscodeReason {
	a, ok := p.audio(s.Codec)
	switch {
	case !ok:
		return []domain.TranscodeReason{domain.AudioCodecNotSupported}
	case a.MaxChannels > 0 && s.Channels > a.MaxChannels:
		return []domain.TranscodeReason{domain.AudioChannelsNotSupported}
	}
	return nil
}

func (p Profile) video(codec string) (VideoSupport, bool) {
	i := slices.IndexFunc(p.Video, func(v VideoSupport) bool { return v.Codec == codec })
	if i < 0 {
		return VideoSupport{}, false
	}
	return p.Video[i], true
}

func (p Profile) audio(codec string) (AudioSupport, bool) {
	i := slices.IndexFunc(p.Audio, func(a AudioSupport) bool { return a.Codec == codec })
	if i < 0 {
		return AudioSupport{}, false
	}
	return p.Audio[i], true
}

// downmixBoost is how much louder sound mixed down to stereo is made, as Jellyfin's
// DownMixAudioBoost: with the centre and surrounds folded in, the dialogue is quiet otherwise.
const downmixBoost = 2

// encoders are the audio codecs the server encodes to, with the most channels FFmpeg's encoder
// writes.
var encoders = map[string]int{"aac": 8, "eac3": 6, "ac3": 6}

// audioEncode chooses what a stream the client cannot take is encoded to: the first codec in the
// client's list the server encodes, as Jellyfin takes a transcoding profile's codecs in order,
// keeping as many of its channels as both allow, at Jellyfin's bitrates.
func (p Profile) audioEncode(s domain.Stream) (domain.AudioEncode, bool) {
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
		enc := domain.AudioEncode{Codec: a.Codec, Channels: channels, BitrateKbps: kbps}
		if channels == 2 && s.Channels > 2 {
			enc.Boost = downmixBoost
		}
		return enc, true
	}
	return domain.AudioEncode{}, false
}

// sourceKbps stands in for a copy whose bitrate is unknown, as Jellyfin's does.
const sourceKbps = 40_000

// videoEncode chooses what video the client cannot take as it is is encoded to: HEVC where the
// client plays it and hevc allows it, as Jellyfin prefers HEVC where it is allowed, else H.264; no
// larger than the client takes it, and spending no more than the client's limit or what the codec
// needs to match the source. HEVC keeps the source's HDR10 or HLG in 10 bits where the client
// shows it; any other HDR is tone mapped to SDR.
func (p Profile) videoEncode(s domain.Stream, copyKbps int, hevc domain.HEVCEncoding) (domain.VideoEncode, bool) {
	codecs := []domain.VideoCodec{domain.VideoH264}
	switch hevc {
	case domain.HEVCAllow:
		codecs = []domain.VideoCodec{domain.VideoHEVC, domain.VideoH264}
	case domain.HEVCDeny:
	}
	for _, codec := range codecs {
		v, ok := p.video(string(codec))
		if !ok {
			continue
		}
		width, height := fit(s.Width, s.Height, v.MaxWidth, v.MaxHeight)
		kbps := scaleBitrate(cmp.Or(copyKbps, sourceKbps), s.Codec, codec)
		if p.MaxBitrateKbps > 0 {
			kbps = min(kbps, p.MaxBitrateKbps)
		}
		r := domain.RangeSDR
		if kept := hdrOf(s); codec == domain.VideoHEVC && kept != domain.RangeSDR && v.showsTen(kept) {
			r = kept
		}
		return domain.VideoEncode{
			Codec: codec, Width: width, Height: height, BitrateKbps: kbps, Range: r,
			ToneMap: s.Range != "" && s.Range != domain.RangeSDR && r == domain.RangeSDR, Deinterlace: s.Interlaced,
		}, true
	}
	return domain.VideoEncode{}, false
}

// hdrOf is the HDR a stream encoded again can keep: HDR10+ its HDR10, and Dolby Vision the base
// layer it is compatible with, as Jellyfin transcodes Dolby Vision 8.1 as HDR10. SDR where there is
// none, Dolby Vision 5's picture being nothing without its RPU.
func hdrOf(s domain.Stream) domain.Range {
	switch s.Range {
	case domain.RangeHDR10, domain.RangeHDR10Plus:
		return domain.RangeHDR10
	case domain.RangeHLG:
		return domain.RangeHLG
	case domain.RangeDV:
		// 6 is a Blu-ray's profile 7, whose base layer is HDR10.
		if dv := s.DolbyVision; dv != nil && (dv.Compatibility == 1 || dv.Compatibility == 6) {
			return domain.RangeHDR10
		} else if dv != nil && dv.Compatibility == 4 {
			return domain.RangeHLG
		}
	case domain.RangeSDR:
	}
	return domain.RangeSDR
}

// showsTen reports whether the client shows a range in 10-bit HEVC: it lists the range, and takes
// Main 10.
func (v VideoSupport) showsTen(r domain.Range) bool {
	return slices.Contains(v.ranges(), r) && (v.MaxBitDepth == 0 || v.MaxBitDepth >= 10) &&
		(len(v.Profiles) == 0 || slices.ContainsFunc(v.Profiles, func(name string) bool { return strings.EqualFold(name, "Main 10") }))
}

// efficiency is how much less than H.264 a codec spends on the same picture, as Jellyfin's
// GetVideoBitrateScaleFactor.
func efficiency(codec string) float64 {
	switch codec {
	case "hevc", "vp9":
		return 0.6
	case "av1":
		return 0.5
	}
	return 1
}

// scaleBitrate is what codec spends to match a source of from at kbps, as Jellyfin's
// ScaleBitrate: more where the source's codec is the more efficient, never less, and more again
// for a source so small that the encode would show its blocks; nothing more from 30 Mbps, where it
// is not seen.
func scaleBitrate(kbps int, from string, to domain.VideoCodec) int {
	factor := max(efficiency(string(to))/efficiency(from), 1)
	switch {
	case kbps <= 500:
		factor = max(factor, 4)
	case kbps <= 1000:
		factor = max(factor, 3)
	case kbps <= 2000:
		factor = max(factor, 2.5)
	case kbps <= 3000:
		factor = max(factor, 2)
	case kbps >= 30_000:
		factor = 1
	}
	return int(math.Round(factor * float64(kbps)))
}

// fit answers a picture's size scaled down to fit within a limit, keeping its shape, each side
// even as 4:2:0 needs. A zero limit is none.
func fit(width, height, maxWidth, maxHeight int) (int, int) {
	scale := 1.0
	if maxWidth > 0 && width > maxWidth {
		scale = float64(maxWidth) / float64(width)
	}
	if maxHeight > 0 && height > maxHeight {
		scale = min(scale, float64(maxHeight)/float64(height))
	}
	even := func(n float64) int { return int(n/2) * 2 }
	return even(float64(width) * scale), even(float64(height) * scale)
}

// audioBudget is the most audio may spend of the client's bitrate, Jellyfin's
// GetMaxAudioBitrateForTotalBitrate; zero is no limit.
func (p Profile) audioBudget() int {
	for _, step := range [][2]int{{640, 128}, {2000, 384}, {3000, 448}, {4000, 640}, {5000, 768}, {10_000, 1536}, {15_000, 2304}, {20_000, 3584}} {
		if p.MaxBitrateKbps > 0 && p.MaxBitrateKbps <= step[0] {
			return step[1]
		}
	}
	if p.MaxBitrateKbps > 0 {
		return 7168
	}
	return 0
}
