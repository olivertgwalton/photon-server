package jellyfin

import (
	"cmp"

	"golang.org/x/text/language"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/hls"
	"github.com/olivertgwalton/photon-server/internal/store"
	"github.com/olivertgwalton/photon-server/internal/words"
)

// mediaSource is Jellyfin's MediaSourceInfo: one copy of a title. Every field the Kotlin SDK keeps
// is written, true or false, for it refuses a source short of any. Infuse takes an item with none
// to be unplayable.
type mediaSource struct {
	Protocol                   string        `json:"Protocol"`
	ID                         string        `json:"Id"`
	Type                       string        `json:"Type"`
	Container                  string        `json:"Container"`
	Path                       string        `json:"Path,omitempty"`
	Size                       int64         `json:"Size,omitempty"`
	Name                       string        `json:"Name"`
	IsRemote                   bool          `json:"IsRemote"`
	ETag                       string        `json:"ETag"`
	RunTimeTicks               int64         `json:"RunTimeTicks,omitempty"`
	ReadAtNativeFramerate      bool          `json:"ReadAtNativeFramerate"`
	IgnoreDts                  bool          `json:"IgnoreDts"`
	IgnoreIndex                bool          `json:"IgnoreIndex"`
	GenPtsInput                bool          `json:"GenPtsInput"`
	SupportsTranscoding        bool          `json:"SupportsTranscoding"`
	SupportsDirectStream       bool          `json:"SupportsDirectStream"`
	SupportsDirectPlay         bool          `json:"SupportsDirectPlay"`
	IsInfiniteStream           bool          `json:"IsInfiniteStream"`
	RequiresOpening            bool          `json:"RequiresOpening"`
	RequiresClosing            bool          `json:"RequiresClosing"`
	RequiresLooping            bool          `json:"RequiresLooping"`
	SupportsProbing            bool          `json:"SupportsProbing"`
	VideoType                  string        `json:"VideoType"`
	MediaStreams               []mediaStream `json:"MediaStreams"`
	MediaAttachments           []struct{}    `json:"MediaAttachments"`
	Formats                    []string      `json:"Formats"`
	Bitrate                    int           `json:"Bitrate,omitempty"`
	RequiredHTTPHeaders        struct{}      `json:"RequiredHttpHeaders"`
	TranscodingURL             string        `json:"TranscodingUrl,omitempty"`
	TranscodingSubProtocol     string        `json:"TranscodingSubProtocol"`
	TranscodingContainer       string        `json:"TranscodingContainer,omitempty"`
	DefaultAudioStreamIndex    *int          `json:"DefaultAudioStreamIndex,omitempty"`
	DefaultSubtitleStreamIndex *int          `json:"DefaultSubtitleStreamIndex,omitempty"`
	HasSegments                bool          `json:"HasSegments"`
}

// mediaStream is Jellyfin's MediaStream: one track of a copy.
type mediaStream struct {
	Codec                  string  `json:"Codec"`
	Language               string  `json:"Language,omitempty"`
	Title                  string  `json:"Title,omitempty"`
	VideoRange             string  `json:"VideoRange,omitempty"`
	VideoRangeType         string  `json:"VideoRangeType,omitempty"`
	DvProfile              int16   `json:"DvProfile,omitempty"`
	DisplayTitle           string  `json:"DisplayTitle"`
	IsInterlaced           bool    `json:"IsInterlaced"`
	ChannelLayout          string  `json:"ChannelLayout,omitempty"`
	BitRate                int     `json:"BitRate,omitempty"`
	BitDepth               int16   `json:"BitDepth,omitempty"`
	Channels               int     `json:"Channels,omitempty"`
	SampleRate             int     `json:"SampleRate,omitempty"`
	IsDefault              bool    `json:"IsDefault"`
	IsForced               bool    `json:"IsForced"`
	IsHearingImpaired      bool    `json:"IsHearingImpaired"`
	Height                 int     `json:"Height,omitempty"`
	Width                  int     `json:"Width,omitempty"`
	RealFrameRate          float64 `json:"RealFrameRate,omitempty"`
	Profile                string  `json:"Profile,omitempty"`
	Type                   string  `json:"Type"`
	Index                  int     `json:"Index"`
	IsExternal             bool    `json:"IsExternal"`
	IsTextSubtitleStream   bool    `json:"IsTextSubtitleStream"`
	SupportsExternalStream bool    `json:"SupportsExternalStream"`
	DeliveryMethod         string  `json:"DeliveryMethod,omitempty"`
	Level                  int     `json:"Level,omitempty"`
}

// streamTypes are Jellyfin's MediaStreamType for each kind of track.
var streamTypes = map[domain.StreamKind]string{
	domain.StreamVideo: "Video", domain.StreamAudio: "Audio", domain.StreamSubtitle: "Subtitle",
}

// ranges are Jellyfin's VideoRange and VideoRangeType for each of photon's ranges. Dolby Vision is
// told by profile below.
var ranges = map[domain.Range][2]string{
	domain.RangeSDR: {"SDR", "SDR"}, domain.RangeHLG: {"HDR", "HLG"}, domain.RangeHDR10: {"HDR", "HDR10"},
	domain.RangeHDR10Plus: {"HDR", "HDR10Plus"}, domain.RangeDV: {"HDR", "DOVI"},
}

// textual reports whether a subtitle is what Jellyfin calls text: one an app draws itself, plain or
// styled, not pictures.
func textual(codec string) bool {
	return hls.TextSubtitle(codec) || hls.StyledSubtitle(codec)
}

// sourceOf is a copy as a media source. Its Path is its file's name alone, as Infuse reads one,
// never where it is on the server.
func sourceOf(v store.VersionPage, w words.Words) mediaSource {
	file := ""
	if len(v.Files) > 0 {
		file = v.Files[0].File
	}
	s := mediaSource{
		Protocol: "File", ID: guid(v.ID), Path: file, Type: "Default", Container: domain.ContainerName(v.Container), Size: v.SizeBytes,
		Name: w.Version(v), ETag: guid(v.ID),
		RunTimeTicks:         v.DurationMS * ticksPerMS,
		SupportsDirectStream: true, SupportsDirectPlay: true, VideoType: "VideoFile",
		MediaStreams: make([]mediaStream, 0, len(v.Streams)), MediaAttachments: []struct{}{}, Formats: []string{},
		Bitrate: v.BitrateKbps * 1000, TranscodingSubProtocol: "http",
		DefaultAudioStreamIndex: v.DefaultAudioStream, DefaultSubtitleStreamIndex: v.DefaultSubtitleStream,
	}
	for _, t := range v.Streams {
		s.MediaStreams = append(s.MediaStreams, streamOf(t, w))
		if t.Kind == domain.StreamAudio && s.DefaultAudioStreamIndex == nil {
			s.DefaultAudioStreamIndex = &t.Index
		}
	}
	// Subtitle files beside the copy follow its own tracks, each at the address an app builds for
	// it from its index and codec.
	base := externalBase(v)
	for n, f := range v.Subtitles {
		m := mediaStream{
			Codec: f.Codec, Language: iso639(f.Language), Title: f.Title, IsDefault: f.Default, IsForced: f.Forced,
			IsHearingImpaired: f.HearingImpaired, Type: "Subtitle", Index: base + n, IsExternal: true,
			IsTextSubtitleStream: textual(f.Codec), SupportsExternalStream: true, DeliveryMethod: "External",
			DisplayTitle: w.SubtitleFile(f),
		}
		if v.DefaultSubtitleFile != nil && *v.DefaultSubtitleFile == f.ID {
			s.DefaultSubtitleStreamIndex = &m.Index
		}
		s.MediaStreams = append(s.MediaStreams, m)
	}
	s.HasSegments = len(v.Markers) > 0
	return s
}

func streamOf(t store.StreamPage, w words.Words) mediaStream {
	m := mediaStream{
		DisplayTitle: w.Stream(t),
		Codec:        t.Codec, Language: iso639(t.Language), Title: t.Title, IsDefault: t.Default, IsForced: t.Forced,
		IsHearingImpaired: t.HearingImpaired, Type: streamTypes[t.Kind], Index: t.Index, Profile: t.Profile,
		Level: t.Level, BitRate: t.BitrateKbps * 1000,
	}
	switch t.Kind {
	case domain.StreamVideo:
		m.Width, m.Height, m.RealFrameRate, m.BitDepth = t.Width, t.Height, t.FrameRate, t.BitDepth
		r := ranges[cmp.Or(t.Range, domain.RangeSDR)]
		m.VideoRange, m.VideoRangeType = r[0], r[1]
		if t.Range == domain.RangeDV {
			m.DvProfile = t.DVProfile
			if t.DVProfile == 7 || t.DVProfile == 8 {
				m.VideoRangeType = "DOVIWithHDR10"
			}
		}
	case domain.StreamAudio:
		m.Channels, m.ChannelLayout, m.SampleRate = t.Channels, t.ChannelLayout, t.SampleRate
	case domain.StreamSubtitle:
		m.IsTextSubtitleStream = textual(t.Codec)
	}
	return m
}

// externalBase is the index of a copy's first subtitle file: after every track of its own.
func externalBase(v store.VersionPage) int {
	base := 0
	for _, t := range v.Streams {
		base = max(base, t.Index+1)
	}
	return base
}

// iso639 is a language as Jellyfin names one, by its three letters: "eng" for photon's "en".
func iso639(tag string) string {
	if tag == "" {
		return ""
	}
	t, err := language.Parse(tag)
	if err != nil {
		return tag
	}
	base, _ := t.Base()
	return base.ISO3()
}
