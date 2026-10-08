package playback

import (
	"cmp"
	"math"
	"slices"
	"strings"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

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
