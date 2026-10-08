package domain

import (
	"strings"
	"time"

	"golang.org/x/text/language"
)

type StreamKind string

const (
	StreamVideo    StreamKind = "video"
	StreamAudio    StreamKind = "audio"
	StreamSubtitle StreamKind = "subtitle"
)

func StreamKinds() []StreamKind {
	return []StreamKind{StreamVideo, StreamAudio, StreamSubtitle}
}

// Range is how a picture's brightness is coded.
type Range string

const (
	RangeSDR       Range = "sdr"
	RangeHLG       Range = "hlg"
	RangeHDR10     Range = "hdr10"
	RangeHDR10Plus Range = "hdr10plus"
	RangeDV        Range = "dv"
)

func Ranges() []Range {
	return []Range{RangeSDR, RangeHLG, RangeHDR10, RangeHDR10Plus, RangeDV}
}

// ContainerName names a container as clients do, from ffprobe's format name, which lists every
// demuxer that reads it. Matroska and WebM share one, and nothing stored tells them apart.
func ContainerName(format string) string {
	name, _, _ := strings.Cut(format, ",")
	switch name {
	case "matroska":
		return "mkv"
	case "mov":
		return "mp4"
	case "mpegts":
		return "ts"
	}
	return name
}

// Facts are what ffprobe says of a file.
type Facts struct {
	Container   string
	Duration    time.Duration
	BitrateKbps int
	Streams     []Stream
	Chapters    []Chapter
}

type Stream struct {
	Index    int
	Kind     StreamKind
	Codec    string
	Profile  string
	Language language.Tag
	Title    string

	Default         bool
	Forced          bool
	HearingImpaired bool
	Commentary      bool

	Width, Height int
	FrameRate     float64
	BitDepth      int
	// Level is ffprobe's: ten times the level for H.264 (41 is 4.1), thirty times for HEVC.
	Level       int
	Range       Range
	DolbyVision *DolbyVision
	// Interlaced is a picture ffprobe gives a field order for; one it calls unknown is taken as
	// progressive.
	Interlaced bool

	Channels      int
	ChannelLayout string
	SampleRate    int
	BitrateKbps   int
}

type DolbyVision struct {
	Profile, Level int
	Compatibility  DolbyVisionCompatibility
}

// DolbyVisionLayers is what a Dolby Vision stream carries beside its RPU: the base layer alone,
// or an enhancement layer too.
type DolbyVisionLayers string

const (
	LayersBase     DolbyVisionLayers = "base"
	LayersEnhanced DolbyVisionLayers = "enhanced"
)

// Layers follows from the profile: 4 and 7 are the dual-layer ones.
func (dv DolbyVision) Layers() DolbyVisionLayers {
	if dv.Profile == 4 || dv.Profile == 7 {
		return LayersEnhanced
	}
	return LayersBase
}

// DolbyVisionCompatibility is what a Dolby Vision stream's base layer shows without its RPU.
type DolbyVisionCompatibility string

const (
	// CompatibleNone is a base layer that is nothing alone, as profile 5's.
	CompatibleNone  DolbyVisionCompatibility = ""
	CompatibleHDR10 DolbyVisionCompatibility = "hdr10"
	CompatibleSDR   DolbyVisionCompatibility = "sdr"
	CompatibleHLG   DolbyVisionCompatibility = "hlg"
	// CompatibleBluRay is an Ultra HD Blu-ray's HDR10, profile 7's.
	CompatibleBluRay DolbyVisionCompatibility = "bluray"
)

// CompatibilityOf reads Dolby's bl_signal_compatibility_id, as ffprobe gives it and the catalogue
// keeps it; an id Dolby reserves is none.
func CompatibilityOf(id int) DolbyVisionCompatibility {
	switch id {
	case 1:
		return CompatibleHDR10
	case 2:
		return CompatibleSDR
	case 4:
		return CompatibleHLG
	case 6:
		return CompatibleBluRay
	}
	return CompatibleNone
}

// ID is Dolby's bl_signal_compatibility_id.
func (c DolbyVisionCompatibility) ID() int {
	switch c {
	case CompatibleHDR10:
		return 1
	case CompatibleSDR:
		return 2
	case CompatibleHLG:
		return 4
	case CompatibleBluRay:
		return 6
	case CompatibleNone:
	}
	return 0
}

type Chapter struct {
	Start, End time.Duration
	Title      string
}

// TagOf is a language's BCP 47 tag, or nothing for none.
func TagOf(l language.Tag) string {
	if l == language.Und {
		return ""
	}
	return l.String()
}
