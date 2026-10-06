package playback

import (
	"context"
	"os"
	"testing"
	"uuid"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/hls"
	"github.com/olivertgwalton/photon-server/internal/media"
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

func (indexed) Keyframes(context.Context, uuid.UUID) ([]int64, bool, error) {
	return []int64{0}, true, nil
}
func (indexed) SaveKeyframes(context.Context, uuid.UUID, []int64) error { return nil }

// opened keeps the HLS each playback was opened as.
type opened map[uuid.UUID]hls.Copy

func (o opened) Open(playback uuid.UUID, c hls.Copy) error {
	o[playback] = c
	return nil
}

func (opened) Close(uuid.UUID) {}

func TestTheMasterPlaylistSaysWhatIsSent(t *testing.T) {
	copyOf := func(c Copy) store.PlayCopy {
		return store.PlayCopy{
			BitrateKbps: c.BitrateKbps, Streams: c.Streams, Parts: []store.PlayPart{{ID: uuid.NewV7(), DurationMS: 60_000}},
			Subtitles: []store.PlaySubtitle{{ID: uuid.NewV7(), Codec: "subrip", Title: "English"}},
		}
	}
	h264 := Copy{BitrateKbps: 8_000, Streams: []media.Stream{
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
			want:      hls.Variant{BandwidthKbps: 40_000, Codecs: []string{"dvh1.08.00", "mlpa"}, Range: "PQ"},
			subtitles: []string{"", "English"},
		},
		{
			name: "Dolby Vision's base layer alone", copy: film,
			video: domain.VideoPlan{Stream: 0, Codec: "hevc", DolbyVision: domain.DolbyVisionStrip}, audio: &domain.AudioPlan{Stream: 2},
			want:      hls.Variant{BandwidthKbps: 40_000, Codecs: []string{"hvc1.2.4.L153.B0", "ac-3"}, Range: "PQ"},
			subtitles: []string{"", "English"},
		},
		{
			name: "H.264 copied, AAC encoded", copy: h264,
			video:     domain.VideoPlan{Stream: 0, Codec: "h264"},
			audio:     &domain.AudioPlan{Stream: 1, Encode: &domain.AudioEncode{Codec: "ac3", Channels: 6, BitrateKbps: 640}},
			want:      hls.Variant{BandwidthKbps: 8_000, Codecs: []string{"avc1.640029", "ac-3"}, Range: "SDR"},
			subtitles: []string{"English"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hlsOf := opened{}
			playback := uuid.NewV7()
			if err := NewRemuxes(indexed{}, nil, hlsOf).Open(t.Context(), playback, copyOf(tc.copy), tc.video, tc.audio); err != nil {
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
