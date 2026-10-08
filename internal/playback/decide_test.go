package playback

import (
	gocmp "cmp"
	"errors"
	"slices"
	"testing"
	"uuid"

	"github.com/google/go-cmp/cmp"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// Subtitle files beside the film: signs and songs in ASS, SubRip, and a picture.
var (
	signsFile  = uuid.NewV7()
	subripFile = uuid.NewV7()
	pgsFile    = uuid.NewV7()
)

// A film as rips have it: Dolby Vision 8.1 HEVC in Matroska, with TrueHD 7.1 and an AC-3 5.1 dub,
// and subtitles of every kind.
var film = Copy{
	Container: "matroska,webm", BitrateKbps: 40_000, Parts: 1,
	Streams: []domain.Stream{
		{
			Index: 0, Kind: domain.StreamVideo, Codec: "hevc", Profile: "Main 10", Level: 153, Width: 3840, Height: 2160,
			FrameRate: 24000.0 / 1001, BitDepth: 10, Range: domain.RangeDV, DolbyVision: &domain.DolbyVision{Profile: 8, Compatibility: domain.CompatibleHDR10},
		},
		{Index: 1, Kind: domain.StreamAudio, Codec: "truehd", Channels: 8, Default: true},
		{Index: 2, Kind: domain.StreamAudio, Codec: "ac3", Channels: 6},
		{Index: 3, Kind: domain.StreamSubtitle, Codec: "subrip"},
		{Index: 4, Kind: domain.StreamSubtitle, Codec: "hdmv_pgs_subtitle"},
		{Index: 5, Kind: domain.StreamSubtitle, Codec: "ass"},
	},
	Files: []SubtitleFile{{ID: signsFile, Codec: "ass"}, {ID: subripFile, Codec: "subrip"}, {ID: pgsFile, Codec: "hdmv_pgs_subtitle"}},
}

// appleTV plays HEVC with Dolby Vision from MP4 but not Matroska, and no TrueHD; it would rather
// have AAC.
var appleTV = Profile{
	Containers: []string{"mp4", "mov"},
	Video: []VideoSupport{
		{Codec: "h264", MaxBitDepth: 8},
		{Codec: "hevc", MaxBitDepth: 10, Ranges: []domain.Range{domain.RangeSDR, domain.RangeHDR10, domain.RangeHLG, domain.RangeDV}},
	},
	Audio: []AudioSupport{{Codec: "aac", MaxChannels: 8}, {Codec: "ac3"}, {Codec: "eac3"}},
}

func TestDecide(t *testing.T) {
	everything := appleTV
	everything.Containers = append(everything.Containers, "matroska")
	everything.Audio = append(everything.Audio, AudioSupport{Codec: "truehd"})
	hdr10Only := appleTV
	hdr10Only.Video = []VideoSupport{{Codec: "hevc", Ranges: []domain.Range{domain.RangeHDR10}}}
	drawsPGS := everything
	drawsPGS.Subtitles = []SubtitleSupport{{Codec: "hdmv_pgs_subtitle", Delivery: domain.SubtitleEmbedded}}
	drawsText := everything
	drawsText.Subtitles = []SubtitleSupport{{Codec: "subrip", Delivery: domain.SubtitleEmbedded}}
	drawsASS := everything
	drawsASS.Subtitles = []SubtitleSupport{{Codec: "ass", Delivery: domain.SubtitleEmbedded}}
	// A browser opens Matroska but draws only what it is given beside the video.
	browser := everything
	browser.Subtitles = []SubtitleSupport{{Codec: "ass", Delivery: domain.SubtitleSidecar}, {Codec: "subrip", Delivery: domain.SubtitleSidecar}}
	browserNoMKV := appleTV
	browserNoMKV.Subtitles = browser.Subtitles
	sdrOnly := appleTV
	sdrOnly.Video = []VideoSupport{{Codec: "hevc"}}
	cappedRemux := appleTV
	cappedRemux.Audio = append(cappedRemux.Audio, AudioSupport{Codec: "truehd"})
	cappedRemux.MaxBitrateKbps = 80_000
	sdrH264 := appleTV
	sdrH264.Video = []VideoSupport{{Codec: "hevc"}, {Codec: "h264", MaxWidth: 1920, MaxHeight: 1080}}
	capped := everything
	capped.MaxBitrateKbps = 20_000
	stereo := appleTV
	stereo.Audio = []AudioSupport{{Codec: "aac", MaxChannels: 2}, {Codec: "ac3", MaxChannels: 2}}
	silent := appleTV
	silent.Audio = nil
	takesTS := everything
	takesTS.Containers = appleTV.Containers
	takesTS.Segments = domain.SegmentsMPEGTS

	hevc := func(dv domain.DolbyVisionHandling) *domain.VideoPlan {
		return &domain.VideoPlan{Stream: 0, Codec: "hevc", DolbyVision: dv}
	}
	for _, tc := range []struct {
		name     string
		profile  Profile
		audio    *int
		subtitle *int
		file     *uuid.UUID
		// hevc is HEVCAllow where it is not said.
		hevc     domain.HEVCEncoding
		noLibass bool
		want     Decision
		err      error
	}{
		{
			name: "a client that plays it all plays the file", profile: everything,
			want: Decision{Method: domain.PlayDirect, Video: hevc(domain.DolbyVisionKeep), Audio: &domain.AudioPlan{Stream: 1}},
		},
		{
			name:    "Matroska the client cannot open is remuxed, and TrueHD it cannot play encoded to its first codec",
			profile: appleTV,
			want: Decision{
				Method: domain.PlayRemux, Video: hevc(domain.DolbyVisionKeep),
				Audio:   &domain.AudioPlan{Stream: 1, Encode: &domain.AudioEncode{Codec: "aac", Channels: 8, BitrateKbps: 640}},
				Reasons: []domain.TranscodeReason{domain.ContainerNotSupported, domain.AudioCodecNotSupported},
			},
		},
		{
			name: "the AC-3 dub asked for is copied", profile: appleTV, audio: new(2),
			want: Decision{
				Method: domain.PlayRemux, Video: hevc(domain.DolbyVisionKeep), Audio: &domain.AudioPlan{Stream: 2},
				Reasons: []domain.TranscodeReason{domain.ContainerNotSupported},
			},
		},
		{
			name: "a stereo client gets stereo AAC", profile: stereo, audio: new(2),
			want: Decision{
				Method: domain.PlayRemux, Video: hevc(domain.DolbyVisionKeep),
				Audio:   &domain.AudioPlan{Stream: 2, Encode: &domain.AudioEncode{Codec: "aac", Channels: 2, BitrateKbps: 256, Boost: 2}},
				Reasons: []domain.TranscodeReason{domain.ContainerNotSupported, domain.AudioChannelsNotSupported},
			},
		},
		{
			name: "an HDR10 client gets Dolby Vision's base layer", profile: hdr10Only, audio: new(2),
			want: Decision{
				Method: domain.PlayRemux, Video: hevc(domain.DolbyVisionStrip), Audio: &domain.AudioPlan{Stream: 2},
				Reasons: []domain.TranscodeReason{domain.ContainerNotSupported},
			},
		},
		{
			name: "HDR to an SDR client of HEVC alone is tone mapped into HEVC", profile: sdrOnly, audio: new(2),
			want: Decision{
				Method: domain.PlayTranscode,
				Video: &domain.VideoPlan{Stream: 0, Codec: "hevc", Encode: &domain.VideoEncode{
					Codec: domain.VideoHEVC, Width: 3840, Height: 2160, BitrateKbps: 40_000, Range: domain.RangeSDR, ToneMap: true,
				}},
				Audio:   &domain.AudioPlan{Stream: 2},
				Reasons: []domain.TranscodeReason{domain.ContainerNotSupported, domain.VideoRangeNotSupported},
			},
		},
		{
			name: "an SDR client of HEVC alone cannot be sent HDR where HEVC is not encoded", profile: sdrOnly, audio: new(2), hevc: domain.HEVCDeny,
			want: Decision{Reasons: []domain.TranscodeReason{domain.ContainerNotSupported, domain.VideoRangeNotSupported}}, err: ErrNoCompatibleStream,
		},
		{
			name:    "a bitrate over the client's limit is encoded to HEVC to fit it, its HDR10 kept, with the audio's share taken first",
			profile: capped,
			want: Decision{
				Method: domain.PlayTranscode,
				Video: &domain.VideoPlan{Stream: 0, Codec: "hevc", Encode: &domain.VideoEncode{
					Codec: domain.VideoHEVC, Width: 3840, Height: 2160, BitrateKbps: 20_000 - 640, Range: domain.RangeHDR10,
				}},
				Audio:   &domain.AudioPlan{Stream: 1, Encode: &domain.AudioEncode{Codec: "aac", Channels: 8, BitrateKbps: 640}},
				Reasons: []domain.TranscodeReason{domain.BitrateExceedsLimit},
			},
		},
		{
			name: "a server that encodes no HEVC tone maps into H.264", profile: capped, hevc: domain.HEVCDeny,
			want: Decision{
				Method: domain.PlayTranscode,
				Video: &domain.VideoPlan{Stream: 0, Codec: "hevc", Encode: &domain.VideoEncode{
					Codec: domain.VideoH264, Width: 3840, Height: 2160, BitrateKbps: 20_000 - 640, Range: domain.RangeSDR, ToneMap: true,
				}},
				Audio:   &domain.AudioPlan{Stream: 1, Encode: &domain.AudioEncode{Codec: "aac", Channels: 8, BitrateKbps: 640}},
				Reasons: []domain.TranscodeReason{domain.BitrateExceedsLimit},
			},
		},
		{
			name: "under a limit it fits, a remux copies audio of a bitrate nobody knows", profile: cappedRemux,
			want: Decision{
				Method: domain.PlayRemux, Video: hevc(domain.DolbyVisionKeep), Audio: &domain.AudioPlan{Stream: 1},
				Reasons: []domain.TranscodeReason{domain.ContainerNotSupported},
			},
		},
		{
			name: "HDR to an SDR client is tone mapped into HEVC, which it would rather have", profile: sdrH264, audio: new(2),
			want: Decision{
				Method: domain.PlayTranscode,
				Video: &domain.VideoPlan{Stream: 0, Codec: "hevc", Encode: &domain.VideoEncode{
					Codec: domain.VideoHEVC, Width: 3840, Height: 2160, BitrateKbps: 40_000, Range: domain.RangeSDR, ToneMap: true,
				}},
				Audio:   &domain.AudioPlan{Stream: 2},
				Reasons: []domain.TranscodeReason{domain.ContainerNotSupported, domain.VideoRangeNotSupported},
			},
		},
		{
			name: "HDR to an SDR client is tone mapped into H.264 its size", profile: sdrH264, audio: new(2), hevc: domain.HEVCDeny,
			want: Decision{
				Method: domain.PlayTranscode,
				Video: &domain.VideoPlan{Stream: 0, Codec: "hevc", Encode: &domain.VideoEncode{
					Codec: domain.VideoH264, Width: 1920, Height: 1080, BitrateKbps: 40_000, Range: domain.RangeSDR, ToneMap: true,
				}},
				Audio:   &domain.AudioPlan{Stream: 2},
				Reasons: []domain.TranscodeReason{domain.ContainerNotSupported, domain.VideoRangeNotSupported},
			},
		},
		{
			name:    "MPEG-TS is sent Dolby Vision's base layer, and TrueHD it cannot carry encoded",
			profile: takesTS,
			want: Decision{
				Method: domain.PlayRemux, Video: hevc(domain.DolbyVisionStrip),
				Audio:   &domain.AudioPlan{Stream: 1, Encode: &domain.AudioEncode{Codec: "aac", Channels: 8, BitrateKbps: 640}},
				Reasons: []domain.TranscodeReason{domain.ContainerNotSupported},
			},
		},
		{
			name: "MPEG-TS copies AC-3", profile: takesTS, audio: new(2),
			want: Decision{
				Method: domain.PlayRemux, Video: hevc(domain.DolbyVisionStrip), Audio: &domain.AudioPlan{Stream: 2},
				Reasons: []domain.TranscodeReason{domain.ContainerNotSupported},
			},
		},
		{
			name: "audio no encoder writes for the client", profile: silent,
			want: Decision{Reasons: []domain.TranscodeReason{domain.ContainerNotSupported, domain.AudioCodecNotSupported}}, err: ErrNoCompatibleStream,
		},
		{name: "an audio stream the copy lacks", profile: everything, audio: new(3), err: ErrNoSuchAudio},
		{name: "a subtitle stream the copy lacks", profile: everything, subtitle: new(1), err: ErrNoSuchSubtitle},
		{
			name: "a picture subtitle the client draws plays in the file", profile: drawsPGS, subtitle: new(4),
			want: Decision{Method: domain.PlayDirect, Video: hevc(domain.DolbyVisionKeep), Audio: &domain.AudioPlan{Stream: 1}},
		},
		{
			name: "a picture subtitle the client cannot draw is drawn in", profile: everything, subtitle: new(4),
			want: Decision{
				Method: domain.PlayTranscode,
				Video: &domain.VideoPlan{Stream: 0, Codec: "hevc", Encode: &domain.VideoEncode{
					Codec: domain.VideoHEVC, Width: 3840, Height: 2160, BitrateKbps: 40_000, Range: domain.RangeHDR10, Burn: new(4),
				}},
				Audio: &domain.AudioPlan{Stream: 1}, Reasons: []domain.TranscodeReason{domain.SubtitleCodecNotSupported},
			},
		},
		{
			name: "plain text the client draws from the file plays in it", profile: drawsText, subtitle: new(3),
			want: Decision{Method: domain.PlayDirect, Video: hevc(domain.DolbyVisionKeep), Audio: &domain.AudioPlan{Stream: 1}},
		},
		{
			name: "plain text the client cannot draw from the file is carried in HLS, the video copied", profile: everything, subtitle: new(3),
			want: Decision{
				Method: domain.PlayRemux, Video: hevc(domain.DolbyVisionKeep), Audio: &domain.AudioPlan{Stream: 1},
				Reasons: []domain.TranscodeReason{domain.SubtitleCodecNotSupported},
			},
		},
		{
			name: "styled text the client draws from the file plays in it", profile: drawsASS, subtitle: new(5),
			want: Decision{Method: domain.PlayDirect, Video: hevc(domain.DolbyVisionKeep), Audio: &domain.AudioPlan{Stream: 1}},
		},
		{
			name: "styled text a client draws beside the video is read out of the file played as it is", profile: browser, subtitle: new(5),
			want: Decision{Method: domain.PlayDirect, Video: hevc(domain.DolbyVisionKeep), Audio: &domain.AudioPlan{Stream: 1}},
		},
		{
			name: "styled text a client draws beside the video goes beside HLS, the video copied", profile: browserNoMKV, subtitle: new(5), audio: new(2),
			want: Decision{
				Method: domain.PlayRemux, Video: hevc(domain.DolbyVisionKeep), Audio: &domain.AudioPlan{Stream: 2},
				Reasons: []domain.TranscodeReason{domain.ContainerNotSupported},
			},
		},
		{
			name: "styled text the client cannot draw is drawn in, never made plain", profile: everything, subtitle: new(5),
			want: Decision{
				Method: domain.PlayTranscode,
				Video: &domain.VideoPlan{Stream: 0, Codec: "hevc", Encode: &domain.VideoEncode{
					Codec: domain.VideoHEVC, Width: 3840, Height: 2160, BitrateKbps: 40_000, Range: domain.RangeHDR10, Burn: new(5),
				}},
				Audio: &domain.AudioPlan{Stream: 1}, Reasons: []domain.TranscodeReason{domain.SubtitleCodecNotSupported},
			},
		},
		{
			name: "styled text the client cannot draw, on a server whose FFmpeg has no libass", profile: everything, subtitle: new(5), noLibass: true,
			want: Decision{Reasons: []domain.TranscodeReason{domain.SubtitleCodecNotSupported}}, err: ErrNoCompatibleStream,
		},
		{
			name: "a styled file the client draws plays beside the file", profile: browser, file: &signsFile,
			want: Decision{Method: domain.PlayDirect, Video: hevc(domain.DolbyVisionKeep), Audio: &domain.AudioPlan{Stream: 1}},
		},
		{
			name: "a styled file the client cannot draw is drawn in", profile: drawsASS, file: &signsFile,
			want: Decision{
				Method: domain.PlayTranscode,
				Video: &domain.VideoPlan{Stream: 0, Codec: "hevc", Encode: &domain.VideoEncode{
					Codec: domain.VideoHEVC, Width: 3840, Height: 2160, BitrateKbps: 40_000, Range: domain.RangeHDR10, BurnFile: &signsFile,
				}},
				Audio: &domain.AudioPlan{Stream: 1}, Reasons: []domain.TranscodeReason{domain.SubtitleCodecNotSupported},
			},
		},
		{
			name: "a plain file the client cannot draw is carried in HLS", profile: drawsText, file: &subripFile,
			want: Decision{
				Method: domain.PlayRemux, Video: hevc(domain.DolbyVisionKeep), Audio: &domain.AudioPlan{Stream: 1},
				Reasons: []domain.TranscodeReason{domain.SubtitleCodecNotSupported},
			},
		},
		{name: "a picture beside the copy is no subtitle a player shows", profile: everything, file: &pgsFile, err: ErrNoSuchSubtitleFile},
		{name: "a subtitle file not beside the copy", profile: everything, file: new(uuid.NewV7()), err: ErrNoSuchSubtitleFile},
	} {
		t.Run(tc.name, func(t *testing.T) {
			enc := Encoding{HEVC: gocmp.Or(tc.hevc, domain.HEVCAllow), Libass: !tc.noLibass}
			got, err := Decide(tc.profile, film, domain.ChosenTracks{Audio: tc.audio, Subtitle: tc.subtitle, SubtitleFile: tc.file}, enc)
			if !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("decision (-want +got):\n%s", diff)
			}
		})
	}
}

func TestAnInterlacedPictureIsDeinterlacedAsItIsEncoded(t *testing.T) {
	broadcast := Copy{Container: "mpegts", BitrateKbps: 8_000, Streams: []domain.Stream{
		{Index: 0, Kind: domain.StreamVideo, Codec: "mpeg2video", Width: 1920, Height: 1080, Interlaced: true},
		{Index: 1, Kind: domain.StreamAudio, Codec: "ac3", Channels: 6},
	}}
	got, err := Decide(appleTV, broadcast, domain.ChosenTracks{}, Encoding{HEVC: domain.HEVCAllow})
	if err != nil {
		t.Fatal(err)
	}
	if got.Video.Encode == nil || !got.Video.Encode.Deinterlace {
		t.Errorf("video = %+v, want it encoded and deinterlaced", got.Video)
	}
}

func TestVideoLimits(t *testing.T) {
	h264 := domain.Stream{Kind: domain.StreamVideo, Codec: "h264", Profile: "High 10", Level: 51, Width: 1920, Height: 1080, BitDepth: 10}
	for _, tc := range []struct {
		support VideoSupport
		want    []domain.TranscodeReason
	}{
		{VideoSupport{Codec: "h264"}, nil},
		{VideoSupport{Codec: "hevc"}, []domain.TranscodeReason{domain.VideoCodecNotSupported}},
		{VideoSupport{Codec: "h264", Profiles: []string{"high", "main"}}, []domain.TranscodeReason{domain.VideoProfileNotSupported}},
		{VideoSupport{Codec: "h264", Profiles: []string{"high 10"}, MaxLevel: 41}, []domain.TranscodeReason{domain.VideoLevelNotSupported}},
		{VideoSupport{Codec: "h264", MaxWidth: 1280, MaxBitDepth: 8}, []domain.TranscodeReason{domain.VideoResolutionNotSupported, domain.VideoBitDepthNotSupported}},
	} {
		p := Profile{Video: []VideoSupport{tc.support}}
		if got := p.videoReasons(h264); !cmp.Equal(got, tc.want) {
			t.Errorf("%+v: %v, want %v", tc.support, got, tc.want)
		}
	}
}

func TestSurroundIsKeptInLayoutsPlayersKnow(t *testing.T) {
	for _, tc := range []struct {
		channels, most int
		want           int
	}{{5, 0, 6}, {5, 5, 2}, {7, 0, 8}, {7, 7, 6}, {3, 0, 2}, {1, 0, 1}} {
		p := Profile{Audio: []AudioSupport{{Codec: "aac", MaxChannels: tc.most}}}
		got, _ := p.audioEncode(domain.Stream{Codec: "dts", Channels: tc.channels})
		if got.Channels != tc.want {
			t.Errorf("%d channels to a client of %d: %d, want %d", tc.channels, tc.most, got.Channels, tc.want)
		}
	}
}

func TestTheEncodeIsGivenTheRoomItNeedsToMatchTheSource(t *testing.T) {
	for _, tc := range []struct {
		codec       string
		to          domain.VideoCodec
		kbps, limit int
		want        int
	}{
		{"hevc", domain.VideoH264, 10_000, 0, 16_667},
		{"av1", domain.VideoH264, 10_000, 0, 20_000},
		{"h264", domain.VideoH264, 10_000, 0, 10_000},
		{"hevc", domain.VideoH264, 1_000, 0, 3_000},
		{"mpeg2video", domain.VideoH264, 400, 0, 1_600},
		{"h264", domain.VideoH264, 2_500, 0, 5_000},
		{"hevc", domain.VideoH264, 50_000, 0, 50_000},
		{"hevc", domain.VideoH264, 10_000, 8_000, 8_000},
		{"hevc", domain.VideoHEVC, 10_000, 0, 10_000},
		{"h264", domain.VideoHEVC, 10_000, 0, 10_000},
		{"av1", domain.VideoHEVC, 10_000, 0, 12_000},
		{"h264", domain.VideoHEVC, 1_000, 0, 3_000},
	} {
		p := Profile{Video: []VideoSupport{{Codec: string(tc.to)}}, MaxBitrateKbps: tc.limit}
		got, _ := p.videoEncode(domain.Stream{Codec: tc.codec, Width: 1920, Height: 1080}, tc.kbps, domain.HEVCAllow)
		if got.Codec != tc.to || got.BitrateKbps != tc.want {
			t.Errorf("%s at %d kbps to %s, limit %d: %s at %d kbps, want %d", tc.codec, tc.kbps, tc.to, tc.limit, got.Codec, got.BitrateKbps, tc.want)
		}
	}
}

// HEVC is chosen over H.264 where the client plays it and the server may encode it, and keeps the
// source's HDR10 or HLG where the client shows it in 10 bits; anything else is tone mapped.
func TestTheEncodeKeepsHDROnlyWhereTheClientShowsIt(t *testing.T) {
	hdr10 := domain.Stream{Codec: "hevc", Range: domain.RangeHDR10}
	hlg := domain.Stream{Codec: "hevc", Range: domain.RangeHLG}
	dv := func(compatibility domain.DolbyVisionCompatibility) domain.Stream {
		return domain.Stream{Codec: "hevc", Range: domain.RangeDV, DolbyVision: &domain.DolbyVision{Profile: 8, Compatibility: compatibility}}
	}
	shows := func(ranges ...domain.Range) VideoSupport { return VideoSupport{Codec: "hevc", Ranges: ranges} }
	h264 := VideoSupport{Codec: "h264"}
	for _, tc := range []struct {
		name    string
		source  domain.Stream
		plays   []VideoSupport
		hevc    domain.HEVCEncoding
		codec   domain.VideoCodec
		r       domain.Range
		toneMap bool
	}{
		{"HDR10 to an HDR10 client", hdr10, []VideoSupport{h264, shows(domain.RangeSDR, domain.RangeHDR10)}, domain.HEVCAllow, domain.VideoHEVC, domain.RangeHDR10, false},
		{"HDR10 where HEVC is denied", hdr10, []VideoSupport{h264, shows(domain.RangeHDR10)}, domain.HEVCDeny, domain.VideoH264, domain.RangeSDR, true},
		{"HDR10 to an SDR HEVC client", hdr10, []VideoSupport{shows()}, domain.HEVCAllow, domain.VideoHEVC, domain.RangeSDR, true},
		{"HDR10 to a client of 8 bits", hdr10, []VideoSupport{{Codec: "hevc", MaxBitDepth: 8, Ranges: []domain.Range{domain.RangeHDR10}}}, domain.HEVCAllow, domain.VideoHEVC, domain.RangeSDR, true},
		{"HDR10 to a client of Main alone", hdr10, []VideoSupport{{Codec: "hevc", Profiles: []string{"Main"}, Ranges: []domain.Range{domain.RangeHDR10}}}, domain.HEVCAllow, domain.VideoHEVC, domain.RangeSDR, true},
		{"HDR10 to an H.264 client", hdr10, []VideoSupport{h264}, domain.HEVCAllow, domain.VideoH264, domain.RangeSDR, true},
		{"HDR10+ as its HDR10", domain.Stream{Codec: "hevc", Range: domain.RangeHDR10Plus}, []VideoSupport{shows(domain.RangeHDR10)}, domain.HEVCAllow, domain.VideoHEVC, domain.RangeHDR10, false},
		{"HLG to an HLG client", hlg, []VideoSupport{shows(domain.RangeHLG)}, domain.HEVCAllow, domain.VideoHEVC, domain.RangeHLG, false},
		{"HLG to an HDR10 client", hlg, []VideoSupport{shows(domain.RangeHDR10)}, domain.HEVCAllow, domain.VideoHEVC, domain.RangeSDR, true},
		{"Dolby Vision 8.1 as HDR10", dv(domain.CompatibleHDR10), []VideoSupport{shows(domain.RangeHDR10)}, domain.HEVCAllow, domain.VideoHEVC, domain.RangeHDR10, false},
		{"Dolby Vision 8.4 as HLG", dv(domain.CompatibleHLG), []VideoSupport{shows(domain.RangeHLG)}, domain.HEVCAllow, domain.VideoHEVC, domain.RangeHLG, false},
		{"Dolby Vision 5, which no base layer shows", dv(domain.CompatibleNone), []VideoSupport{shows(domain.RangeHDR10, domain.RangeDV)}, domain.HEVCAllow, domain.VideoHEVC, domain.RangeSDR, true},
		{"SDR to HEVC", domain.Stream{Codec: "mpeg2video"}, []VideoSupport{h264, shows()}, domain.HEVCAllow, domain.VideoHEVC, domain.RangeSDR, false},
	} {
		got, ok := Profile{Video: tc.plays}.videoEncode(tc.source, 8000, tc.hevc)
		if !ok || got.Codec != tc.codec || got.Range != tc.r || got.ToneMap != tc.toneMap {
			t.Errorf("%s: %s in %s, tone mapped %t (%t); want %s in %s, tone mapped %t", tc.name, got.Codec, got.Range, got.ToneMap, ok, tc.codec, tc.r, tc.toneMap)
		}
	}
	if _, ok := (Profile{Video: []VideoSupport{shows(domain.RangeHDR10)}}).videoEncode(hdr10, 8000, domain.HEVCDeny); ok {
		t.Error("a client of HEVC alone is encoded for where HEVC is denied")
	}
}

func TestAPictureFitsWithinTheClientsLimit(t *testing.T) {
	for _, tc := range []struct{ w, h, maxW, maxH, wantW, wantH int }{
		{3840, 1600, 1920, 1080, 1920, 800},
		{1440, 1080, 1920, 720, 960, 720},
		{1280, 720, 1920, 1080, 1280, 720},
		{1281, 721, 0, 0, 1280, 720},
	} {
		if w, h := fit(tc.w, tc.h, tc.maxW, tc.maxH); w != tc.wantW || h != tc.wantH {
			t.Errorf("%dx%d within %dx%d: %dx%d, want %dx%d", tc.w, tc.h, tc.maxW, tc.maxH, w, h, tc.wantW, tc.wantH)
		}
	}
}

// A copy in two files plays as it is only on a client that plays each in turn; another has them
// joined into one stream, with the video copied.
func TestACopyInSeveralFilesIsJoinedForAClientThatPlaysOne(t *testing.T) {
	twoFiles := film
	twoFiles.Parts = 2
	everything := appleTV
	everything.Containers = append(everything.Containers, "matroska")
	everything.Audio = append(everything.Audio, AudioSupport{Codec: "truehd"})
	if d, err := Decide(everything, twoFiles, domain.ChosenTracks{}, Encoding{HEVC: domain.HEVCAllow}); err != nil || d.Method != domain.PlayRemux || d.Video.Encode != nil ||
		!slices.Equal(d.Reasons, []domain.TranscodeReason{domain.PartsNotSupported}) {
		t.Errorf("joined: %+v, %v; want a remux, for the parts", d, err)
	}
	everything.Parts = domain.PartsEach
	if d, err := Decide(everything, twoFiles, domain.ChosenTracks{}, Encoding{HEVC: domain.HEVCAllow}); err != nil || d.Method != domain.PlayDirect {
		t.Errorf("each in turn: %+v, %v; want the files as they are", d, err)
	}
	// A styled stream is in each file, on each one's clock, and is drawn in rather than read out.
	everything.Subtitles = []SubtitleSupport{{Codec: "ass", Delivery: domain.SubtitleSidecar}}
	signs := domain.ChosenTracks{Subtitle: new(5)}
	if d, err := Decide(everything, twoFiles, signs, Encoding{HEVC: domain.HEVCAllow, Libass: true}); err != nil || d.Video.Encode == nil || d.Video.Encode.Burn == nil {
		t.Errorf("styled text in each file: %+v, %v; want it drawn in", d, err)
	}
}

// MPEG-TS carries H.264 and HEVC without Dolby Vision, and the audio players take from it:
// anything else a client plays from fragmented MP4 is encoded for one that asks for MPEG-TS.
func TestMPEGTSSegmentsCarryWhatPlayersTakeFromThem(t *testing.T) {
	plays := Profile{
		Containers: []string{"mp4"},
		Video: []VideoSupport{
			{Codec: "h264"},
			{Codec: "av1"},
			{Codec: "hevc", Ranges: []domain.Range{domain.RangeSDR, domain.RangeHDR10, domain.RangeDV}},
		},
		Audio: []AudioSupport{{Codec: "aac"}, {Codec: "opus"}, {Codec: "flac"}},
	}
	in := func(video domain.Stream, audio string) Copy {
		video.Kind = domain.StreamVideo
		return Copy{Container: "matroska,webm", BitrateKbps: 10_000, Streams: []domain.Stream{
			video, {Index: 1, Kind: domain.StreamAudio, Codec: audio, Channels: 2},
		}}
	}
	dv5 := domain.Stream{Codec: "hevc", Range: domain.RangeDV, DolbyVision: &domain.DolbyVision{Profile: 5}}
	for _, tc := range []struct {
		name         string
		copy         Copy
		videoEncoded bool
		audioEncoded bool
	}{
		{"H.264 and AAC", in(domain.Stream{Codec: "h264"}, "aac"), false, false},
		{"AV1 and Opus", in(domain.Stream{Codec: "av1"}, "opus"), true, true},
		{"HEVC and FLAC", in(domain.Stream{Codec: "hevc"}, "flac"), false, true},
		{"Dolby Vision 5, which has no base layer to send", in(dv5, "aac"), true, false},
	} {
		fmp4, err := Decide(plays, tc.copy, domain.ChosenTracks{}, Encoding{HEVC: domain.HEVCAllow})
		if err != nil {
			t.Fatalf("%s in fragmented MP4: %v", tc.name, err)
		}
		if fmp4.Video.Encode != nil || fmp4.Audio.Encode != nil {
			t.Errorf("%s in fragmented MP4: video %+v, audio %+v; want both copied", tc.name, fmp4.Video, fmp4.Audio)
		}
		ts := plays
		ts.Segments = domain.SegmentsMPEGTS
		got, err := Decide(ts, tc.copy, domain.ChosenTracks{}, Encoding{HEVC: domain.HEVCAllow})
		if err != nil {
			t.Fatalf("%s in MPEG-TS: %v", tc.name, err)
		}
		if (got.Video.Encode != nil) != tc.videoEncoded || got.Video.DolbyVision != domain.DolbyVisionNone || (got.Audio.Encode != nil) != tc.audioEncoded {
			t.Errorf("%s in MPEG-TS: video %+v, audio %+v; want video encoded %t with no Dolby Vision, audio encoded %t",
				tc.name, got.Video, got.Audio, tc.videoEncoded, tc.audioEncoded)
		}
		if e := got.Audio.Encode; e != nil && e.Codec != "aac" {
			t.Errorf("%s in MPEG-TS: audio encoded to %s, want the client's AAC", tc.name, e.Codec)
		}
	}
}
