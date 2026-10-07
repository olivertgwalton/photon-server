package jellyfin

import (
	"slices"
	"strconv"
	"strings"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/playback"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// deviceProfile is Jellyfin's DeviceProfile: what an app plays as it is, the limits it sets on each
// codec and container, the HLS it takes, and how it takes each kind of subtitle.
type deviceProfile struct {
	MaxStreamingBitrate int64                `json:"MaxStreamingBitrate"`
	DirectPlayProfiles  []directPlayProfile  `json:"DirectPlayProfiles"`
	TranscodingProfiles []transcodingProfile `json:"TranscodingProfiles"`
	ContainerProfiles   []containerProfile   `json:"ContainerProfiles"`
	CodecProfiles       []codecProfile       `json:"CodecProfiles"`
	SubtitleProfiles    []subtitleProfile    `json:"SubtitleProfiles"`
}

type directPlayProfile struct {
	Container  string `json:"Container"`
	VideoCodec string `json:"VideoCodec"`
	AudioCodec string `json:"AudioCodec"`
	Type       string `json:"Type"`
}

type transcodingProfile struct {
	Container        string `json:"Container"`
	Type             string `json:"Type"`
	VideoCodec       string `json:"VideoCodec"`
	AudioCodec       string `json:"AudioCodec"`
	Protocol         string `json:"Protocol"`
	MaxAudioChannels string `json:"MaxAudioChannels"`
}

type containerProfile struct {
	Type       string      `json:"Type"`
	Container  string      `json:"Container"`
	Conditions []condition `json:"Conditions"`
}

type codecProfile struct {
	Type            string      `json:"Type"`
	Codec           string      `json:"Codec"`
	Container       string      `json:"Container"`
	Conditions      []condition `json:"Conditions"`
	ApplyConditions []condition `json:"ApplyConditions"`
}

type subtitleProfile struct {
	Format string `json:"Format"`
	Method string `json:"Method"`
}

// condition is Jellyfin's ProfileCondition. One that leaves IsRequired out is required, as
// Jellyfin's own constructor makes it.
type condition struct {
	Condition  string `json:"Condition"`
	Property   string `json:"Property"`
	Value      string `json:"Value"`
	IsRequired *bool  `json:"IsRequired"`
}

// list is a profile's comma list, lower case.
func list(s string) []string {
	var out []string
	for v := range strings.SplitSeq(strings.ToLower(s), ",") {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// matches is whether a profile's list admits any of names: an empty list admits everything, and a
// list that starts with "-" everything but what it names ("-mp3,flac" is all but MP3 and FLAC).
func matches(profile string, names ...string) bool {
	l := list(profile)
	if len(l) == 0 {
		return true
	}
	if first, ok := strings.CutPrefix(l[0], "-"); ok {
		l[0] = first
		return !slices.ContainsFunc(names, func(n string) bool { return slices.Contains(l, n) })
	}
	return slices.ContainsFunc(names, func(n string) bool { return slices.Contains(l, n) })
}

// containerNames are the names Jellyfin's apps call a copy's container by, from FFmpeg's.
func containerNames(format string) []string {
	var out []string
	for name := range strings.SplitSeq(format, ",") {
		switch name {
		case "matroska":
			out = append(out, "mkv", "matroska")
		case "webm":
			out = append(out, "webm")
		case "mov", "mp4", "m4a":
			out = append(out, "mp4", "m4v", "mov")
		case "mpegts":
			out = append(out, "ts", "mpegts", "m2ts")
		default:
			out = append(out, name)
		}
	}
	return out
}

// codecNames are the names an app may call a codec by: Jellyfin's apps write dca for DTS.
func codecNames(codec string) []string {
	if codec == "dts" {
		return []string{"dts", "dca"}
	}
	return []string{codec}
}

// rangeType is a picture's dynamic range as Jellyfin's VideoRangeType names it.
func rangeType(s domain.Stream) string {
	switch s.Range {
	case domain.RangeSDR:
		return "SDR"
	case domain.RangeHDR10:
		return "HDR10"
	case domain.RangeHDR10Plus:
		return "HDR10Plus"
	case domain.RangeHLG:
		return "HLG"
	case domain.RangeDV:
		dv := s.DolbyVision
		switch {
		case dv == nil:
			return "DOVI"
		case dv.EnhancementLayer:
			return "DOVIWithEL"
		case dv.Compatibility == 1:
			return "DOVIWithHDR10"
		case dv.Compatibility == 2:
			return "DOVIWithSDR"
		case dv.Compatibility == 4:
			return "DOVIWithHLG"
		}
		return "DOVI"
	}
	return "Unknown"
}

// holds is whether a track meets a condition, as Jellyfin's ConditionProcessor judges: one about
// what is not known of the track holds unless it is required.
func (c condition) holds(s domain.Stream) bool {
	required := c.IsRequired == nil || *c.IsRequired
	op, want := strings.ToLower(c.Condition), c.Value
	number := func(have float64, known bool) bool {
		if !known {
			return !required
		}
		if op == "equalsany" {
			return slices.ContainsFunc(strings.Split(want, "|"), func(v string) bool {
				n, err := strconv.ParseFloat(v, 64)
				return err == nil && n == have
			})
		}
		n, err := strconv.ParseFloat(want, 64)
		if err != nil {
			return false
		}
		switch op {
		case "equals":
			return have == n
		case "notequals":
			return have != n
		case "lessthanequal":
			return have <= n
		case "greaterthanequal":
			return have >= n
		}
		return false
	}
	text := func(have string) bool {
		if have == "" {
			return !required
		}
		switch op {
		case "equals":
			return strings.EqualFold(have, want)
		case "notequals":
			return !strings.EqualFold(have, want)
		case "equalsany":
			return slices.ContainsFunc(strings.Split(want, "|"), func(v string) bool { return strings.EqualFold(v, have) })
		}
		return false
	}
	switch strings.ToLower(c.Property) {
	case "width":
		return number(float64(s.Width), s.Width > 0)
	case "height":
		return number(float64(s.Height), s.Height > 0)
	case "videolevel":
		return number(float64(s.Level), s.Level > 0)
	case "videobitdepth":
		return number(float64(s.BitDepth), s.BitDepth > 0)
	case "videoframerate":
		return number(s.FrameRate, s.FrameRate > 0)
	case "videobitrate", "audiobitrate":
		return number(float64(s.BitrateKbps)*1000, s.BitrateKbps > 0)
	case "audiochannels":
		return number(float64(s.Channels), s.Channels > 0)
	case "audiosamplerate":
		return number(float64(s.SampleRate), s.SampleRate > 0)
	case "videoprofile", "audioprofile":
		// Apps write ffprobe's profiles lower case and often without spaces: "main 10", "main10".
		return text(strings.ReplaceAll(s.Profile, " ", "")) || text(s.Profile)
	case "isinterlaced":
		return text(strconv.FormatBool(s.Interlaced))
	case "videorangetype":
		have := rangeType(s)
		if have == "Unknown" {
			return !required
		}
		// HDR10+ satisfies what HDR10 does, as its pictures are HDR10's with more.
		names := []string{have}
		if have == "HDR10Plus" {
			names = append(names, "HDR10")
		}
		values := strings.Split(want, "|")
		in := slices.ContainsFunc(names, func(n string) bool {
			return slices.ContainsFunc(values, func(v string) bool { return strings.EqualFold(v, n) })
		})
		if op == "notequals" {
			return !in
		}
		return in
	}
	// What photon does not know of a track (its reference frames, anamorphism, codec tag) holds
	// unless it is required, as Jellyfin judges what it does not know.
	return !required
}

func allHold(cs []condition, s domain.Stream) bool {
	for _, c := range cs {
		if !c.holds(s) {
			return false
		}
	}
	return true
}

// codecAllows is whether a track meets every codec profile of a kind that applies to it in a
// container: those naming its codec, or none, whose ApplyConditions it meets.
func (d deviceProfile) codecAllows(kind string, s domain.Stream, containers []string) bool {
	for _, cp := range d.CodecProfiles {
		if !strings.EqualFold(cp.Type, kind) || !matches(cp.Codec, codecNames(s.Codec)...) ||
			!matches(cp.Container, containers...) || !allHold(cp.ApplyConditions, s) {
			continue
		}
		if !allHold(cp.Conditions, s) {
			return false
		}
	}
	return true
}

// playsDirectly is whether the app plays a copy as it is, its video and audio the tracks chosen:
// one of its direct-play profiles takes the copy's container and the tracks' codecs, and every
// condition its container and codec profiles set holds of them; the copy is one file within the
// bitrate the app takes; and the subtitle chosen, if any, is one it draws itself.
func (d deviceProfile) playsDirectly(c store.PlayCopy, video, audio *domain.Stream, subtitle *subtitleChoice, maxBitrate int64) bool {
	if len(c.Parts) != 1 || maxBitrate > 0 && int64(c.BitrateKbps)*1000 > maxBitrate {
		return false
	}
	if subtitle != nil && !d.subtitleTaken(subtitle.codec, subtitle.external) {
		return false
	}
	containers := containerNames(c.Container)
	for _, dp := range d.DirectPlayProfiles {
		if !strings.EqualFold(dp.Type, "Video") || !matches(dp.Container, containers...) {
			continue
		}
		if video != nil && (!matches(dp.VideoCodec, codecNames(video.Codec)...) || !d.codecAllows("Video", *video, containers) || !d.containerAllows(*video, containers)) {
			continue
		}
		if audio != nil && (!matches(dp.AudioCodec, codecNames(audio.Codec)...) || !d.codecAllows("VideoAudio", *audio, containers)) {
			continue
		}
		return true
	}
	return false
}

// containerAllows is whether a copy's video meets what the app's container profiles set of its
// container.
func (d deviceProfile) containerAllows(video domain.Stream, containers []string) bool {
	for _, cp := range d.ContainerProfiles {
		if strings.EqualFold(cp.Type, "Video") && matches(cp.Container, containers...) && !allHold(cp.Conditions, video) {
			return false
		}
	}
	return true
}

// subtitleChoice is the subtitle an app chose: its codec, and whether it is a file beside the copy.
type subtitleChoice struct {
	codec    string
	external bool
}

// subtitleTaken is whether the app draws a subtitle of a codec itself: from inside the file it
// plays, or a file beside it.
func (d deviceProfile) subtitleTaken(codec string, external bool) bool {
	method := "embed"
	if external {
		method = "external"
	}
	return slices.ContainsFunc(d.SubtitleProfiles, func(s subtitleProfile) bool {
		return strings.EqualFold(s.Method, method) && slices.Contains(subtitleFormats(codec), strings.ToLower(s.Format))
	})
}

// subtitleFormats are the names Jellyfin's apps give a subtitle codec's format.
func subtitleFormats(codec string) []string {
	switch codec {
	case "subrip":
		return []string{"srt", "subrip"}
	case "webvtt":
		return []string{"vtt", "webvtt"}
	case "hdmv_pgs_subtitle":
		return []string{"pgssub", "pgs"}
	case "dvd_subtitle":
		return []string{"dvdsub", "vobsub"}
	case "dvb_subtitle":
		return []string{"dvbsub"}
	}
	return []string{codec}
}

// hlsCodecs are the video codecs Jellyfin carries in HLS, of those an app's transcoding profile
// names.
var hlsCodecs = []string{"h264", "hevc", "av1", "vp9"}

// hls is the app's HLS transcoding profile photon makes: the first of its video profiles over HLS
// in fragmented MP4. ok is false where it takes none.
func (d deviceProfile) hls() (transcodingProfile, bool) {
	for _, t := range d.TranscodingProfiles {
		if strings.EqualFold(t.Type, "Video") && strings.EqualFold(t.Protocol, "hls") && slices.Contains(list(t.Container), "mp4") {
			return t, true
		}
	}
	return transcodingProfile{}, false
}

// hlsProfile is photon's profile for the HLS an app takes, for a copy: the codecs its transcoding
// profile names, within the limits its codec profiles set on them (where their ApplyConditions
// hold of the copy's video), and its bitrate. It opens no container, so photon's decision makes
// HLS of the copy, copying what the app plays and encoding the rest.
func (d deviceProfile) hlsProfile(t transcodingProfile, c store.PlayCopy, maxBitrate int64) playback.Profile {
	p := playback.Profile{MaxBitrateKbps: int(maxBitrate / 1000), Parts: domain.PartsJoined}
	var video domain.Stream
	for _, s := range c.Streams {
		if s.Kind == domain.StreamVideo {
			video = s
			break
		}
	}
	containers := []string{strings.ToLower(t.Container), "hls"}
	for _, codec := range list(t.VideoCodec) {
		if slices.Contains(hlsCodecs, codec) {
			p.Video = append(p.Video, d.videoLimits(codec, video, containers))
		}
	}
	channels, _ := strconv.Atoi(t.MaxAudioChannels)
	for _, codec := range list(t.AudioCodec) {
		a := playback.AudioSupport{Codec: strings.ReplaceAll(codec, "dca", "dts"), MaxChannels: channels}
		for _, cp := range d.CodecProfiles {
			if strings.EqualFold(cp.Type, "VideoAudio") && matches(cp.Codec, codecNames(a.Codec)...) && matches(cp.Container, containers...) {
				for _, cond := range cp.Conditions {
					if strings.EqualFold(cond.Property, "AudioChannels") && strings.EqualFold(cond.Condition, "LessThanEqual") {
						if n, err := strconv.Atoi(cond.Value); err == nil && (a.MaxChannels == 0 || n < a.MaxChannels) {
							a.MaxChannels = n
						}
					}
				}
			}
		}
		p.Audio = append(p.Audio, a)
	}
	return p
}

// videoLimits are the limits an app's video codec profiles set on a codec it takes in HLS, as
// photon's encoder keeps to them. With no condition on its dynamic range, it takes any.
func (d deviceProfile) videoLimits(codec string, video domain.Stream, containers []string) playback.VideoSupport {
	v := playback.VideoSupport{Codec: codec}
	ranged := false
	for _, cp := range d.CodecProfiles {
		if !strings.EqualFold(cp.Type, "Video") || !matches(cp.Codec, codec) || !matches(cp.Container, containers...) ||
			!allHold(cp.ApplyConditions, video) {
			continue
		}
		for _, c := range cp.Conditions {
			n, _ := strconv.Atoi(c.Value)
			op := strings.ToLower(c.Condition)
			switch strings.ToLower(c.Property) {
			case "width":
				if op == "lessthanequal" {
					v.MaxWidth = n
				}
			case "height":
				if op == "lessthanequal" {
					v.MaxHeight = n
				}
			case "videolevel":
				if op == "lessthanequal" {
					v.MaxLevel = n
				}
			case "videobitdepth":
				if op == "lessthanequal" {
					v.MaxBitDepth = n
				}
			case "videoprofile":
				if op == "equalsany" || op == "equals" {
					v.Profiles = strings.Split(c.Value, "|")
				}
			case "videorangetype":
				ranged = true
				v.Ranges = rangesOf(op, c.Value, v.Ranges)
			}
		}
	}
	if !ranged {
		v.Ranges = domain.Ranges()
	}
	return v
}

// rangesOf narrows the ranges a codec takes by a condition on VideoRangeType: those it names, or
// every range but those.
func rangesOf(op, value string, have []domain.Range) []domain.Range {
	named := map[domain.Range]bool{}
	for t := range strings.SplitSeq(value, "|") {
		switch strings.ToLower(t) {
		case "sdr":
			named[domain.RangeSDR] = true
		case "hdr10":
			named[domain.RangeHDR10] = true
		case "hdr10plus":
			named[domain.RangeHDR10Plus] = true
		case "hlg":
			named[domain.RangeHLG] = true
		case "dovi", "doviwithhdr10", "doviwithhlg", "doviwithsdr", "doviwithel", "doviwithhdr10plus", "doviwithelhdr10plus":
			named[domain.RangeDV] = true
		}
	}
	var out []domain.Range
	for _, r := range domain.Ranges() {
		if op == "notequals" != named[r] && (len(have) == 0 || slices.Contains(have, r)) {
			out = append(out, r)
		}
	}
	return out
}
