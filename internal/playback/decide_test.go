package playback

import (
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/media"
)

// A film as rips have it: Dolby Vision 8.1 HEVC in Matroska, with TrueHD 7.1 and an AC-3 5.1 dub.
var film = Copy{
	Container: "matroska,webm", BitrateKbps: 40_000,
	Streams: []media.Stream{
		{
			Index: 0, Kind: domain.StreamVideo, Codec: "hevc", Profile: "Main 10", Level: 153, Width: 3840, Height: 2160,
			BitDepth: 10, Range: domain.RangeDV, DolbyVision: &media.DolbyVision{Profile: 8, Compatibility: 1},
		},
		{Index: 1, Kind: domain.StreamAudio, Codec: "truehd", Channels: 8, Default: true},
		{Index: 2, Kind: domain.StreamAudio, Codec: "ac3", Channels: 6},
		{Index: 3, Kind: domain.StreamSubtitle, Codec: "subrip"},
		{Index: 4, Kind: domain.StreamSubtitle, Codec: "hdmv_pgs_subtitle"},
	},
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
	drawsPGS.Subtitles = []string{"hdmv_pgs_subtitle"}
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

	hevc := func(dv domain.DolbyVisionHandling) *domain.VideoPlan {
		return &domain.VideoPlan{Stream: 0, Codec: "hevc", DolbyVision: dv}
	}
	for _, tc := range []struct {
		name     string
		profile  Profile
		audio    *int
		subtitle *int
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
				Reasons: []Reason{ContainerNotSupported, AudioCodecNotSupported},
			},
		},
		{
			name: "the AC-3 dub asked for is copied", profile: appleTV, audio: new(2),
			want: Decision{
				Method: domain.PlayRemux, Video: hevc(domain.DolbyVisionKeep), Audio: &domain.AudioPlan{Stream: 2},
				Reasons: []Reason{ContainerNotSupported},
			},
		},
		{
			name: "a stereo client gets stereo AAC", profile: stereo, audio: new(2),
			want: Decision{
				Method: domain.PlayRemux, Video: hevc(domain.DolbyVisionKeep),
				Audio:   &domain.AudioPlan{Stream: 2, Encode: &domain.AudioEncode{Codec: "aac", Channels: 2, BitrateKbps: 256}},
				Reasons: []Reason{ContainerNotSupported, AudioChannelsNotSupported},
			},
		},
		{
			name: "an HDR10 client gets Dolby Vision's base layer", profile: hdr10Only, audio: new(2),
			want: Decision{
				Method: domain.PlayRemux, Video: hevc(domain.DolbyVisionStrip), Audio: &domain.AudioPlan{Stream: 2},
				Reasons: []Reason{ContainerNotSupported},
			},
		},
		{
			name: "an SDR client cannot be sent HDR as it is", profile: sdrOnly, audio: new(2),
			want: Decision{Reasons: []Reason{ContainerNotSupported, VideoRangeNotSupported}}, err: ErrNoCompatibleStream,
		},
		{
			name: "a bitrate over the client's limit is encoded to fit it, with the audio's share taken first", profile: capped,
			want: Decision{
				Method: domain.PlayTranscode,
				Video: &domain.VideoPlan{Stream: 0, Codec: "hevc", Encode: &domain.VideoEncode{
					Codec: "h264", Width: 3840, Height: 2160, BitrateKbps: 20_000 - 640, ToneMap: true,
				}},
				Audio:   &domain.AudioPlan{Stream: 1, Encode: &domain.AudioEncode{Codec: "aac", Channels: 8, BitrateKbps: 640}},
				Reasons: []Reason{BitrateExceedsLimit},
			},
		},
		{
			name: "under a limit it fits, a remux copies audio of a bitrate nobody knows", profile: cappedRemux,
			want: Decision{
				Method: domain.PlayRemux, Video: hevc(domain.DolbyVisionKeep), Audio: &domain.AudioPlan{Stream: 1},
				Reasons: []Reason{ContainerNotSupported},
			},
		},
		{
			name: "HDR to an SDR client is tone mapped into H.264 its size", profile: sdrH264, audio: new(2),
			want: Decision{
				Method: domain.PlayTranscode,
				Video: &domain.VideoPlan{Stream: 0, Codec: "hevc", Encode: &domain.VideoEncode{
					Codec: "h264", Width: 1920, Height: 1080, BitrateKbps: 40_000 * 10 / 6, ToneMap: true,
				}},
				Audio:   &domain.AudioPlan{Stream: 2},
				Reasons: []Reason{ContainerNotSupported, VideoRangeNotSupported},
			},
		},
		{
			name: "audio no encoder writes for the client", profile: silent,
			want: Decision{Reasons: []Reason{ContainerNotSupported, AudioCodecNotSupported}}, err: ErrNoCompatibleStream,
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
					Codec: "h264", Width: 3840, Height: 2160, BitrateKbps: 40_000 * 10 / 6, ToneMap: true, Burn: new(4),
				}},
				Audio: &domain.AudioPlan{Stream: 1}, Reasons: []Reason{SubtitleCodecNotSupported},
			},
		},
		{
			name: "a text subtitle is no reason to encode", profile: everything, subtitle: new(3),
			want: Decision{Method: domain.PlayDirect, Video: hevc(domain.DolbyVisionKeep), Audio: &domain.AudioPlan{Stream: 1}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Decide(tc.profile, film, tc.audio, tc.subtitle)
			if !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("decision (-want +got):\n%s", diff)
			}
		})
	}
}

func TestVideoLimits(t *testing.T) {
	h264 := media.Stream{Kind: domain.StreamVideo, Codec: "h264", Profile: "High 10", Level: 51, Width: 1920, Height: 1080, BitDepth: 10}
	for _, tc := range []struct {
		support VideoSupport
		want    []Reason
	}{
		{VideoSupport{Codec: "h264"}, nil},
		{VideoSupport{Codec: "hevc"}, []Reason{VideoCodecNotSupported}},
		{VideoSupport{Codec: "h264", Profiles: []string{"high", "main"}}, []Reason{VideoProfileNotSupported}},
		{VideoSupport{Codec: "h264", Profiles: []string{"high 10"}, MaxLevel: 41}, []Reason{VideoLevelNotSupported}},
		{VideoSupport{Codec: "h264", MaxWidth: 1280, MaxBitDepth: 8}, []Reason{VideoResolutionNotSupported, VideoBitDepthNotSupported}},
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
		got, _ := p.audioEncode(media.Stream{Codec: "dts", Channels: tc.channels})
		if got.Channels != tc.want {
			t.Errorf("%d channels to a client of %d: %d, want %d", tc.channels, tc.most, got.Channels, tc.want)
		}
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
