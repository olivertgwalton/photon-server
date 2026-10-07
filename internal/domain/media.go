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
	Profile, Level   int
	Compatibility    int // the base layer's signal: 0 none, 1 HDR10, 2 SDR, 4 HLG
	BaseLayer        bool
	EnhancementLayer bool
	RPU              bool
}

type Chapter struct {
	Start, End time.Duration
	Title      string
}
