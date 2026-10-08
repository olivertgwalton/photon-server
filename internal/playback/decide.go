package playback

import (
	"errors"
	"slices"
	"strings"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/hls"
	"github.com/olivertgwalton/photon-server/internal/store"
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
	// Subtitles are the subtitle formats it draws itself, and from where.
	Subtitles []SubtitleSupport `json:"subtitles,omitzero"`
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

// SubtitleSupport is a subtitle format a client draws itself, by FFmpeg's name ("subrip", "ass",
// "hdmv_pgs_subtitle"): from inside a file it plays as it is, or from a file of its own beside
// the video, a file beside the copy or a styled stream read out of it.
type SubtitleSupport struct {
	Codec    string                  `json:"codec"`
	Delivery domain.SubtitleDelivery `json:"delivery"`
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
	// ErrNoSuchSubtitleFile is a subtitle file asked for that is not a text file beside the copy.
	ErrNoSuchSubtitleFile = errors.New("playback: the copy has no such text subtitle file")
)

// Copy is what deciding needs of a copy: its container, its bitrate, how many files it is in, its
// first part's streams, the parts of one copy being cut from one master, and the subtitle files
// beside it.
type Copy struct {
	Container   string
	BitrateKbps int
	Parts       int
	Streams     []domain.Stream
	Files       []SubtitleFile
}

// CopyOf is what deciding needs of a copy to play.
func CopyOf(c store.PlayCopy) Copy {
	files := make([]SubtitleFile, len(c.Subtitles))
	for i, f := range c.Subtitles {
		files[i] = SubtitleFile{ID: f.ID, Codec: f.Codec}
	}
	return Copy{Container: c.Container, BitrateKbps: c.BitrateKbps, Parts: len(c.Parts), Streams: c.Streams, Files: files}
}

// SubtitleFile is a subtitle file beside a copy.
type SubtitleFile struct {
	ID    uuid.UUID
	Codec string
}

// Encoding is what the server can make video with: HEVC where it allows it, and styled subtitles
// drawn in where its FFmpeg has libass.
type Encoding struct {
	HEVC   domain.HEVCEncoding
	Libass bool
}

// Decision is how a copy reaches a client: its file as it is, its video copied into HLS, or its
// video encoded again, and why it could not go as it is.
type Decision struct {
	Method  domain.PlayMethod
	Video   *domain.VideoPlan
	Audio   *domain.AudioPlan
	Reasons []domain.TranscodeReason
}

// Sidecar reports whether a client draws a subtitle from a file of its own beside the video: a
// file beside the copy in a format it draws so, or a styled stream of a copy in one file, read out
// as it is. A stream of a copy in several files is in each of them, timed on each one's clock, and
// is never read out into one file.
func (p Profile) Sidecar(codec string, file bool, parts int) bool {
	return p.Draws(codec, domain.SubtitleSidecar) && (file || hls.StyledSubtitle(codec) && parts == 1)
}

func (p Profile) Draws(codec string, d domain.SubtitleDelivery) bool {
	return slices.Contains(p.Subtitles, SubtitleSupport{Codec: codec, Delivery: d})
}

// Carried are the codecs HLS segments of a format carry: fragmented MP4 nearly any, MPEG-TS those
// players take from it, which is no Dolby Vision either.
func Carried(f domain.SegmentFormat) (video, audio []string) {
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
// it does not; else in HLS with its video encoded, to HEVC where the client plays it and the
// server allows it, else to H.264. ErrNoCompatibleStream, with the reasons it could not play as it
// is, where the client takes none of these.
//
// A subtitle asked for, a stream of the file or a text file beside it, reaches the client as it
// draws it: from the file played as it is, or from a file of its own beside the video. Where it
// cannot, plain text is carried in HLS as WebVTT, and a picture (PGS, DVD) or styled text (ASS) is
// drawn into the video, as Jellyfin's subtitle Encode method draws them: WebVTT carries neither.
func Decide(p Profile, c Copy, tracks domain.ChosenTracks, enc Encoding) (Decision, error) {
	video, sound := Pick(c.Streams, tracks.Audio)
	if tracks.Audio != nil && sound == nil {
		return Decision{}, ErrNoSuchAudio
	}
	sub, err := c.subtitle(tracks)
	if err != nil {
		return Decision{}, err
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
	// Drawn in, a subtitle needs the video encoded, as HLS carries neither pictures nor styles.
	burn := false
	if sub != nil {
		sidecar := p.Sidecar(sub.codec, sub.file != nil, c.Parts)
		if !sidecar && (sub.file != nil || !p.Draws(sub.codec, domain.SubtitleEmbedded)) {
			d.Reasons = append(d.Reasons, domain.SubtitleCodecNotSupported)
		}
		burn = !sidecar && !hls.TextSubtitle(sub.codec)
	}
	if len(d.Reasons) == 0 {
		d.Method = domain.PlayDirect
		return d, nil
	}
	if video == nil {
		return Decision{Reasons: d.Reasons}, ErrNoCompatibleStream
	}
	d.Method = domain.PlayRemux
	carriedVideo, carriedAudio := Carried(p.Segments)
	// MPEG-TS has no Dolby Vision: the client is sent the base layer where it shows that alone.
	lostDV := false
	if p.Segments == domain.SegmentsMPEGTS && d.Video.DolbyVision == domain.DolbyVisionKeep {
		if v, _ := p.video(video.Codec); v.showsBase(video.DolbyVision) {
			d.Video.DolbyVision = domain.DolbyVisionStrip
		} else {
			lostDV = true
		}
	}
	if len(videoReasons) > 0 || tooMuch || burn || lostDV || !slices.Contains(carriedVideo, video.Codec) {
		// Styled text is drawn by libass, which an FFmpeg without it cannot.
		if burn && hls.StyledSubtitle(sub.codec) && !enc.Libass {
			return Decision{Reasons: d.Reasons}, ErrNoCompatibleStream
		}
		e, ok := p.videoEncode(*video, c.BitrateKbps, enc.HEVC)
		if !ok {
			return Decision{Reasons: d.Reasons}, ErrNoCompatibleStream
		}
		if burn {
			e.Burn, e.BurnFile = sub.stream, sub.file
		}
		d.Method, d.Video.Encode, d.Video.DolbyVision = domain.PlayTranscode, &e, domain.DolbyVisionNone
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

// chosenSubtitle is the subtitle a play asks for: a stream of the file, or a file beside it.
type chosenSubtitle struct {
	codec  string
	stream *int
	file   *uuid.UUID
}

// subtitle finds the subtitle tracks ask for, nil for none. A picture beside the copy is not one: a
// player draws only a picture inside the file.
func (c Copy) subtitle(tracks domain.ChosenTracks) (*chosenSubtitle, error) {
	switch {
	case tracks.Subtitle != nil:
		i := slices.IndexFunc(c.Streams, func(s domain.Stream) bool {
			return s.Kind == domain.StreamSubtitle && s.Index == *tracks.Subtitle
		})
		if i < 0 {
			return nil, ErrNoSuchSubtitle
		}
		return &chosenSubtitle{codec: c.Streams[i].Codec, stream: tracks.Subtitle}, nil
	case tracks.SubtitleFile != nil:
		i := slices.IndexFunc(c.Files, func(f SubtitleFile) bool { return f.ID == *tracks.SubtitleFile })
		if i < 0 || !hls.TextSubtitle(c.Files[i].Codec) && !hls.StyledSubtitle(c.Files[i].Codec) {
			return nil, ErrNoSuchSubtitleFile
		}
		return &chosenSubtitle{codec: c.Files[i].Codec, file: tracks.SubtitleFile}, nil
	}
	return nil, nil
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

// Pick finds the first video stream and the audio stream to play.
func Pick(streams []domain.Stream, audio *int) (video, sound *domain.Stream) {
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
