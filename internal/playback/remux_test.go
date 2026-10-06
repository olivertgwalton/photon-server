package playback

import (
	"context"
	"os"
	"testing"
	"time"
	"uuid"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/hls"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// indexed is a catalogue whose every part has its keyframes indexed.
type indexed struct{}

func (indexed) PartFile(context.Context, uuid.UUID) (string, string, error) {
	return "", "", os.ErrNotExist
}

func (indexed) SubtitleFile(context.Context, uuid.UUID) (string, string, error) {
	return "", "", os.ErrNotExist
}

func (indexed) Keyframes(context.Context, uuid.UUID) (store.PartKeyframes, error) {
	return store.PartKeyframes{Mode: domain.KeyframesIndex, PtsMS: []int64{0}}, nil
}

func (indexed) AskKeyframes(context.Context, uuid.UUID) error { return nil }

// opened keeps the HLS each playback was opened as.
type opened map[uuid.UUID]hls.Copy

func (o opened) Open(playback uuid.UUID, c hls.Copy) error {
	o[playback] = c
	return nil
}

func TestTheMasterPlaylistSaysWhatIsSent(t *testing.T) {
	copyOf := func(c Copy) store.PlayCopy {
		return store.PlayCopy{
			BitrateKbps: c.BitrateKbps, Streams: c.Streams, Parts: []store.PlayPart{{ID: uuid.NewV7(), DurationMS: 60_000}},
			Subtitles: []store.PlaySubtitle{{ID: uuid.NewV7(), Codec: "subrip", Title: "English"}},
		}
	}
	h264 := Copy{BitrateKbps: 8_000, Streams: []domain.Stream{
		{Index: 0, Kind: domain.StreamVideo, Codec: "h264", Profile: "High", Level: 41, Range: domain.RangeSDR},
		{Index: 1, Kind: domain.StreamAudio, Codec: "aac", Profile: "LC"},
	}}
	for _, tc := range []struct {
		name      string
		copy      Copy
		video     domain.VideoPlan
		audio     *domain.AudioPlan
		want      hls.Variant
		subtitles []string
	}{
		{
			name: "Dolby Vision and TrueHD copied", copy: film,
			video: domain.VideoPlan{Stream: 0, Codec: "hevc", DolbyVision: domain.DolbyVisionKeep}, audio: &domain.AudioPlan{Stream: 1},
			want:      hls.Variant{BandwidthKbps: 40_000, Codecs: []string{"dvh1.08.00", "mlpa"}, Range: "PQ", Width: 3840, Height: 2160, FrameRate: 24000.0 / 1001},
			subtitles: []string{"", "English"},
		},
		{
			name: "Dolby Vision's base layer alone", copy: film,
			video: domain.VideoPlan{Stream: 0, Codec: "hevc", DolbyVision: domain.DolbyVisionStrip}, audio: &domain.AudioPlan{Stream: 2},
			want:      hls.Variant{BandwidthKbps: 40_000, Codecs: []string{"hvc1.2.4.L153.B0", "ac-3"}, Range: "PQ", Width: 3840, Height: 2160, FrameRate: 24000.0 / 1001},
			subtitles: []string{"", "English"},
		},
		{
			name: "H.264 copied, AAC encoded", copy: h264,
			video:     domain.VideoPlan{Stream: 0, Codec: "h264"},
			audio:     &domain.AudioPlan{Stream: 1, Encode: &domain.AudioEncode{Codec: "ac3", Channels: 6, BitrateKbps: 640}},
			want:      hls.Variant{BandwidthKbps: 8_000, Codecs: []string{"avc1.640029", "ac-3"}, Range: "SDR"},
			subtitles: []string{"English"},
		},
		{
			name: "a picture subtitle drawn into 4K is the only subtitle", copy: film,
			video: domain.VideoPlan{Stream: 0, Codec: "hevc", Encode: &domain.VideoEncode{
				Codec: domain.VideoH264, Width: 3840, Height: 2160, BitrateKbps: 20_000, Range: domain.RangeSDR, ToneMap: true, Burn: new(4),
			}},
			audio: &domain.AudioPlan{Stream: 1, Encode: &domain.AudioEncode{Codec: "aac", Channels: 2, BitrateKbps: 256}},
			want:  hls.Variant{BandwidthKbps: 20_256, Codecs: []string{"avc1.640033", "mp4a.40.2"}, Range: "SDR", Width: 3840, Height: 2160, FrameRate: 24000.0 / 1001},
		},
		{
			name: "HDR10 kept in HEVC", copy: film,
			video: domain.VideoPlan{Stream: 0, Codec: "hevc", Encode: &domain.VideoEncode{
				Codec: domain.VideoHEVC, Width: 1920, Height: 1080, BitrateKbps: 8_000, Range: domain.RangeHDR10,
			}},
			audio:     &domain.AudioPlan{Stream: 2},
			want:      hls.Variant{BandwidthKbps: 8_000, Codecs: []string{"hvc1.2.4.L123.B0", "ac-3"}, Range: "PQ", Width: 1920, Height: 1080, FrameRate: 24000.0 / 1001},
			subtitles: []string{"", "English"},
		},
		{
			name: "HDR tone mapped into HEVC", copy: film,
			video: domain.VideoPlan{Stream: 0, Codec: "hevc", Encode: &domain.VideoEncode{
				Codec: domain.VideoHEVC, Width: 3840, Height: 2160, BitrateKbps: 8_000, Range: domain.RangeSDR, ToneMap: true,
			}},
			audio:     &domain.AudioPlan{Stream: 2},
			want:      hls.Variant{BandwidthKbps: 8_000, Codecs: []string{"hvc1.1.6.L153.B0", "ac-3"}, Range: "SDR", Width: 3840, Height: 2160, FrameRate: 24000.0 / 1001},
			subtitles: []string{"", "English"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hlsOf := opened{}
			playback := uuid.NewV7()
			if err := NewRemuxes(indexed{}, hlsOf).Open(t.Context(), playback, copyOf(tc.copy), tc.video, tc.audio); err != nil {
				t.Fatal(err)
			}
			got := hlsOf[playback]
			if diff := cmp.Diff(tc.want, got.Variant); diff != "" {
				t.Errorf("variant (-want +got):\n%s", diff)
			}
			var names []string
			for _, s := range got.Subtitles {
				names = append(names, s.Name)
			}
			if diff := cmp.Diff(tc.subtitles, names, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("subtitles (-want +got):\n%s", diff)
			}
		})
	}
}

type knownKeyframes struct {
	known store.PartKeyframes
	asked bool
}

func (k *knownKeyframes) PartFile(context.Context, uuid.UUID) (string, string, error) {
	return "", "", store.ErrNotFound
}

func (k *knownKeyframes) SubtitleFile(context.Context, uuid.UUID) (string, string, error) {
	return "", "", store.ErrNotFound
}

func (k *knownKeyframes) Keyframes(context.Context, uuid.UUID) (store.PartKeyframes, error) {
	return k.known, nil
}

func (k *knownKeyframes) AskKeyframes(context.Context, uuid.UUID) error {
	k.asked = true
	return nil
}

// A copied video is cut at its keyframes where they are known, and every segment length where
// none are; a play never reads the file for them, and moves a part not reached yet to the front.
func TestACopyIsCutAtTheKeyframesItsLibraryFound(t *testing.T) {
	const duration = 20 * time.Second
	cases := []struct {
		name  string
		known store.PartKeyframes
		want  []time.Duration
		asked bool
	}{
		{"known", store.PartKeyframes{Mode: domain.KeyframesIndex, PtsMS: []int64{0, 7000}}, []time.Duration{0, 7 * time.Second}, false},
		{"none known", store.PartKeyframes{Mode: domain.KeyframesFull, PtsMS: []int64{}}, hls.Forced(duration), false},
		{"not read yet", store.PartKeyframes{Mode: domain.KeyframesIndex}, hls.Forced(duration), true},
		{"off", store.PartKeyframes{Mode: domain.KeyframesOff}, hls.Forced(duration), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			parts := &knownKeyframes{known: c.known}
			hlsOf, playback := opened{}, uuid.NewV7()
			copied := store.PlayCopy{Parts: []store.PlayPart{{ID: uuid.NewV7(), DurationMS: duration.Milliseconds()}}}
			if err := NewRemuxes(parts, hlsOf).Open(t.Context(), playback, copied, domain.VideoPlan{Codec: "h264"}, nil); err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(c.want, hlsOf[playback].Parts[0].Part.Keyframes); diff != "" {
				t.Errorf("keyframes (-want +got):\n%s", diff)
			}
			if parts.asked != c.asked {
				t.Errorf("asked for the part's keyframes: %t, want %t", parts.asked, c.asked)
			}
		})
	}
}
