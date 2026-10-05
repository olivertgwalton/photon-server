package playback

import (
	"testing"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/media"
)

func TestADownloadIsTheFileUnlessItIsOverTheQuality(t *testing.T) {
	hd := []media.Stream{{Index: 0, Kind: domain.StreamVideo, Codec: "hevc", Width: 1920, Height: 1080}}
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
// tone mapped, with AAC taking its share of the bitrate.
func TestAConversionIsATranscodeWithinTheQuality(t *testing.T) {
	c := Copy{Container: "matroska,webm", BitrateKbps: 40_000, Streams: []media.Stream{
		{
			Index: 0, Kind: domain.StreamVideo, Codec: "hevc", Width: 3840, Height: 2160, Range: domain.RangeDV,
			DolbyVision: &media.DolbyVision{Profile: 8, Compatibility: 1},
		},
		{Index: 1, Kind: domain.StreamAudio, Codec: "truehd", Channels: 8},
	}}
	d, err := Conversion(c, domain.Quality{MaxBitrateKbps: 2000, MaxWidth: 1280})
	if err != nil {
		t.Fatal(err)
	}
	v, a := d.Video.Encode, d.Audio.Encode
	if d.Method != domain.PlayTranscode || v == nil || v.Codec != "h264" || v.Width != 1280 || v.Height != 720 || !v.ToneMap ||
		d.Video.DolbyVision != domain.DolbyVisionNone || a == nil || a.Codec != "aac" || v.BitrateKbps+a.BitrateKbps > 2000 {
		t.Errorf("conversion = %+v, video %+v, audio %+v", d, v, a)
	}
}
