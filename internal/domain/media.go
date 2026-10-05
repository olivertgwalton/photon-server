package domain

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
