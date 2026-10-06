package playback

import (
	"testing"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

func TestADownloadIsTheFileUnlessItIsOverTheQuality(t *testing.T) {
	hd := []domain.Stream{{Index: 0, Kind: domain.StreamVideo, Codec: "hevc", Width: 1920, Height: 1080}}
	for _, tc := range []struct {
		name string
		copy Copy
		q    domain.Quality
		fits bool
	}{
		{"under both, whatever its codec", Copy{BitrateKbps: 1500, Streams: hd}, domain.Quality{MaxBitrateKbps: 2000, MaxWidth: 1920}, true},
		{"over the bitrate", Copy{BitrateKbps: 8000, Streams: hd}, domain.Quality{MaxBitrateKbps: 2000}, false},
		{"wider than asked", Copy{BitrateKbps: 1500, Streams: hd}, domain.Quality{MaxBitrateKbps: 2000, MaxWidth: 1280}, false},
		{"of a bitrate nobody knows", Copy{Streams: hd}, domain.Quality{MaxBitrateKbps: 2000}, false},
	} {
		if got := Fits(tc.copy, tc.q); got != tc.fits {
			t.Errorf("%s: fits %t, want %t", tc.name, got, tc.fits)
		}
	}
}

// A 4K Dolby Vision copy with TrueHD 7.1 downloaded at 2 Mbps and 1280 wide is H.264 at that size,
// tone mapped, with AAC taking its share of the bitrate, for a device of H.264; for one of HEVC
// and HDR10 it is HEVC with its HDR kept.
func TestAConversionIsATranscodeWithinTheQuality(t *testing.T) {
	c := Copy{Container: "matroska,webm", BitrateKbps: 40_000, Streams: []domain.Stream{
		{
			Index: 0, Kind: domain.StreamVideo, Codec: "hevc", Width: 3840, Height: 2160, Range: domain.RangeDV,
			DolbyVision: &domain.DolbyVision{Profile: 8, Compatibility: 1},
		},
		{Index: 1, Kind: domain.StreamAudio, Codec: "truehd", Channels: 8},
	}}
	q := domain.Quality{MaxBitrateKbps: 2000, MaxWidth: 1280}
	for _, tc := range []struct {
		plays   []VideoSupport
		codec   domain.VideoCodec
		r       domain.Range
		toneMap bool
	}{
		{[]VideoSupport{{Codec: "h264"}}, domain.VideoH264, domain.RangeSDR, true},
		{[]VideoSupport{{Codec: "h264"}, {Codec: "hevc", Ranges: []domain.Range{domain.RangeSDR, domain.RangeHDR10}}}, domain.VideoHEVC, domain.RangeHDR10, false},
		{Converted(domain.Quality{Codec: domain.VideoHEVC, Range: domain.RangeHDR10}), domain.VideoHEVC, domain.RangeHDR10, false},
	} {
		d, err := Conversion(c, q, tc.plays, domain.HEVCAllow)
		if err != nil {
			t.Fatal(err)
		}
		v, a := d.Video.Encode, d.Audio.Encode
		if d.Method != domain.PlayTranscode || v == nil || v.Codec != tc.codec || v.Range != tc.r || v.ToneMap != tc.toneMap ||
			v.Width != 1280 || v.Height != 720 || d.Video.DolbyVision != domain.DolbyVisionNone || a == nil || a.Codec != "aac" ||
			v.BitrateKbps+a.BitrateKbps > 2000 {
			t.Errorf("%+v: conversion = %+v, video %+v, audio %+v", tc.plays, d, v, a)
		}
	}
}
