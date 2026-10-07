package jellyfin

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
)

func profileOf(t *testing.T, body string) deviceProfile {
	t.Helper()
	var p deviceProfile
	if err := json.Unmarshal([]byte(body), &p); err != nil {
		t.Fatal(err)
	}
	return p
}

// A condition holds of a track as Jellyfin judges it: what is not known of the track holds unless
// required, a condition leaving IsRequired out is required, HDR10+ is HDR10 too, and lists are
// split on "|".
func TestConditionsAreJudgedAsJellyfinJudgesThem(t *testing.T) {
	hevc := domain.Stream{Kind: domain.StreamVideo, Codec: "hevc", Profile: "Main 10", Level: 153, Width: 3840, BitDepth: 10, Range: domain.RangeHDR10Plus}
	no, yes := false, true
	for _, tc := range []struct {
		c    condition
		want bool
	}{
		{condition{Condition: "LessThanEqual", Property: "VideoLevel", Value: "153", IsRequired: &no}, true},
		{condition{Condition: "LessThanEqual", Property: "VideoLevel", Value: "150", IsRequired: &no}, false},
		{condition{Condition: "LessThanEqual", Property: "Width", Value: "1920", IsRequired: &no}, false},
		{condition{Condition: "EqualsAny", Property: "VideoProfile", Value: "main|main 10", IsRequired: &no}, true},
		{condition{Condition: "EqualsAny", Property: "VideoProfile", Value: "main|main10", IsRequired: &no}, true},
		{condition{Condition: "EqualsAny", Property: "VideoRangeType", Value: "SDR|HDR10", IsRequired: &no}, true},
		{condition{Condition: "NotEquals", Property: "VideoRangeType", Value: "HDR10", IsRequired: &no}, false},
		{condition{Condition: "Equals", Property: "RefFrames", Value: "4", IsRequired: &no}, true},
		{condition{Condition: "Equals", Property: "RefFrames", Value: "4", IsRequired: &yes}, false},
		{condition{Condition: "Equals", Property: "RefFrames", Value: "4"}, false},
		{condition{Condition: "Equals", Property: "IsInterlaced", Value: "false", IsRequired: &no}, true},
	} {
		if got := tc.c.holds(hevc); got != tc.want {
			t.Errorf("%+v: %v, want %v", tc.c, got, tc.want)
		}
	}
	dv := domain.Stream{Kind: domain.StreamVideo, Codec: "hevc", Range: domain.RangeDV, DolbyVision: &domain.DolbyVision{Profile: 8, Compatibility: 1}}
	if !(condition{Condition: "EqualsAny", Property: "VideoRangeType", Value: "DOVIWithHDR10", IsRequired: &no}).holds(dv) {
		t.Error("Dolby Vision profile 8.1 is not DOVIWithHDR10")
	}
}

func TestListsAdmitAsJellyfinsDo(t *testing.T) {
	for _, tc := range []struct {
		list  string
		names []string
		want  bool
	}{
		{"", []string{"mkv"}, true},
		{"mp4,m4v", []string{"mkv", "matroska"}, false},
		{"MP4,mkv", []string{"mkv"}, true},
		{"-mp3,flac", []string{"aac"}, true},
		{"-mp3,flac", []string{"flac"}, false},
	} {
		if got := matches(tc.list, tc.names...); got != tc.want {
			t.Errorf("%q admits %v: %v, want %v", tc.list, tc.names, got, tc.want)
		}
	}
}

// Swiftfin's native player, as its profile says: MP4 with H.264 or HEVC and AAC, and HLS in
// fragmented MP4 for the rest.
const swiftfin = `{
	"MaxStreamingBitrate": 120000000,
	"DirectPlayProfiles": [{"Type": "Video", "Container": "mp4,m4v,mov", "VideoCodec": "h264,hevc", "AudioCodec": "aac,ac3,eac3"}],
	"TranscodingProfiles": [{"Type": "Video", "Container": "mp4", "Protocol": "hls", "VideoCodec": "hevc,h264", "AudioCodec": "aac,ac3,eac3", "MaxAudioChannels": "6"}],
	"CodecProfiles": [{"Type": "Video", "Codec": "hevc", "Conditions": [
		{"Condition": "LessThanEqual", "Property": "VideoLevel", "Value": "153", "IsRequired": false}]}],
	"SubtitleProfiles": [{"Format": "vtt", "Method": "Hls"}]
}`

// An app plays a copy as it is only where one of its direct-play profiles takes all of it, and
// otherwise is given HLS within the limits its profiles set.
func TestAnAppsProfileDecidesHowACopyPlays(t *testing.T) {
	p := profileOf(t, swiftfin)
	video := domain.Stream{Index: 0, Kind: domain.StreamVideo, Codec: "hevc", Level: 150, Range: domain.RangeSDR}
	audio := domain.Stream{Index: 1, Kind: domain.StreamAudio, Codec: "aac", Channels: 2}
	copyOf := func(container string, parts int) store.PlayCopy {
		return store.PlayCopy{Container: container, BitrateKbps: 8000, Parts: make([]store.PlayPart, parts), Streams: []domain.Stream{video, audio}}
	}
	if !p.playsDirectly(copyOf("mov,mp4,m4a,3gp,3g2,mj2", 1), &video, &audio, nil, p.MaxStreamingBitrate) {
		t.Error("an MP4 of HEVC and AAC does not play as it is")
	}
	if p.playsDirectly(copyOf("matroska,webm", 1), &video, &audio, nil, p.MaxStreamingBitrate) {
		t.Error("an MKV plays as it is in a player that opens no MKV")
	}
	if p.playsDirectly(copyOf("mov,mp4,m4a,3gp,3g2,mj2", 2), &video, &audio, nil, p.MaxStreamingBitrate) {
		t.Error("a copy in two files plays as one file")
	}
	high := video
	high.Level = 183
	if p.playsDirectly(copyOf("mov,mp4,m4a,3gp,3g2,mj2", 1), &high, &audio, nil, p.MaxStreamingBitrate) {
		t.Error("HEVC above the level the player decodes plays as it is")
	}
	if p.playsDirectly(copyOf("mov,mp4,m4a,3gp,3g2,mj2", 1), &video, &audio, &subtitleChoice{codec: "subrip"}, p.MaxStreamingBitrate) {
		t.Error("a subtitle the player draws only from HLS plays in the file")
	}
	if p.playsDirectly(copyOf("mov,mp4,m4a,3gp,3g2,mj2", 1), &video, &audio, nil, 4_000_000) {
		t.Error("a copy past the bitrate asked for plays as it is")
	}

	tp, ok := p.hls()
	if !ok {
		t.Fatal("Swiftfin takes no HLS photon makes")
	}
	hp := p.hlsProfile(tp, copyOf("matroska,webm", 1), p.MaxStreamingBitrate)
	if hp.MaxBitrateKbps != 120000 || len(hp.Containers) != 0 || len(hp.Video) != 2 || hp.Video[0].Codec != "hevc" || hp.Video[0].MaxLevel != 153 {
		t.Errorf("HLS profile: %+v", hp)
	}
	if !slices.Equal(hp.Video[0].Ranges, domain.Ranges()) || len(hp.Audio) != 3 || hp.Audio[0].MaxChannels != 6 {
		t.Errorf("HLS profile's ranges %v and audio %+v", hp.Video[0].Ranges, hp.Audio)
	}
}

// An app that plays Dolby Vision only as some of its kinds says so by a condition its other
// conditions apply under, as Android TV's does; photon leaves the rest out of the HLS it makes.
func TestRangesAnAppTakesNarrowWhatHLSCarries(t *testing.T) {
	p := profileOf(t, `{
		"TranscodingProfiles": [{"Type": "Video", "Container": "ts,mp4", "Protocol": "hls", "VideoCodec": "hevc,h264", "AudioCodec": "aac"}],
		"CodecProfiles": [{"Type": "Video", "Codec": "hevc",
			"ApplyConditions": [{"Condition": "EqualsAny", "Property": "VideoRangeType", "Value": "DOVI|DOVIWithHDR10|DOVIWithEL", "IsRequired": false}],
			"Conditions": [{"Condition": "NotEquals", "Property": "VideoRangeType", "Value": "DOVI|DOVIWithEL", "IsRequired": false}]}]
	}`)
	tp, ok := p.hls()
	if !ok {
		t.Fatal("no HLS in fragmented MP4")
	}
	dv := domain.Stream{Kind: domain.StreamVideo, Codec: "hevc", Range: domain.RangeDV, DolbyVision: &domain.DolbyVision{Profile: 5}}
	hp := p.hlsProfile(tp, store.PlayCopy{Streams: []domain.Stream{dv}}, 0)
	if slices.Contains(hp.Video[0].Ranges, domain.RangeDV) || !slices.Contains(hp.Video[0].Ranges, domain.RangeHDR10) {
		t.Errorf("ranges for a Dolby Vision profile 5 copy: %v", hp.Video[0].Ranges)
	}
}
