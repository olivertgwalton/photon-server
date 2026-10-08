package words

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"golang.org/x/text/language"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
)

func TestATrackIsNamedAsPlexNamesOne(t *testing.T) {
	w := In(language.English)
	for _, c := range []struct {
		stream domain.Stream
		want   string
	}{
		{domain.Stream{Kind: domain.StreamAudio, Codec: "ac3", Language: language.French, Channels: 6}, "French (Dolby Digital 5.1)"},
		{
			domain.Stream{Kind: domain.StreamAudio, Codec: "aac", Language: language.English, Channels: 2, Title: "Director's commentary", Commentary: true},
			"Director's commentary (English AAC Stereo)",
		},
		{domain.Stream{Kind: domain.StreamAudio, Codec: "opus", Channels: 3, ChannelLayout: "2.1"}, "Unknown (OPUS 2.1)"},
		{domain.Stream{Kind: domain.StreamSubtitle, Codec: "subrip", Language: language.English, HearingImpaired: true}, "English SDH (SRT)"},
		{domain.Stream{Kind: domain.StreamSubtitle, Codec: "hdmv_pgs_subtitle", Language: language.Spanish, Forced: true}, "Spanish Forced (PGS)"},
		// A scope master is 4K by its width, as the wall files it.
		{domain.Stream{Kind: domain.StreamVideo, Codec: "hevc", Width: 3840, Height: 1600, Range: domain.RangeDV}, "4K (HEVC Dolby Vision)"},
		{domain.Stream{Kind: domain.StreamVideo, Codec: "h264", Width: 1920, Height: 1080, Range: domain.RangeSDR}, "1080p (H.264)"},
	} {
		if got := w.Stream(c.stream); got != c.want {
			t.Errorf("Stream(%+v) = %q, want %q", c.stream, got, c.want)
		}
	}
	file := store.SubtitleRef{Codec: "subrip", Language: "fr", Forced: true}
	if got := w.SubtitleFile(file); got != "French Forced (SRT External)" {
		t.Errorf("a subtitle file beside the copy: %q", got)
	}
}

func TestACopyIsNamedByItsLabelAndPicture(t *testing.T) {
	w := In(language.English)
	uhd := []domain.Stream{{Kind: domain.StreamVideo, Codec: "hevc", Width: 3840, Range: domain.RangeHDR10}}
	for _, c := range []struct {
		version store.VersionPage
		want    string
	}{
		{store.VersionPage{Edition: "Director's Cut", Streams: uhd}, "Director's Cut (4K HEVC HDR10)"},
		{store.VersionPage{Streams: uhd}, "4K (HEVC HDR10)"},
		// A label that is the picture's own name is said once.
		{store.VersionPage{Label: "4k", Streams: uhd}, "4K (HEVC HDR10)"},
	} {
		if got := w.Version(c.version); got != c.want {
			t.Errorf("Version(%+v) = %q, want %q", c.version, got, c.want)
		}
	}
}

// A reader reads language names in their own language, the first of theirs that can be named,
// and English where none can.
func TestLanguagesAreNamedInTheReadersLanguage(t *testing.T) {
	for header, want := range map[string]string{
		"fr-CA,fr;q=0.9,en;q=0.8": "Allemand",
		"de":                      "Deutsch",
		"":                        "German",
		"tlh":                     "German",
	} {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("Accept-Language", header)
		if got := Negotiate(httptest.NewRecorder(), r).Language("de"); got != want {
			t.Errorf("Accept-Language %q names German %q, want %q", header, got, want)
		}
	}
	// The answer says whose words it holds, and that a cache keeps one a language.
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Accept-Language", "fr-CA,fr;q=0.9")
	rec := httptest.NewRecorder()
	Negotiate(rec, r)
	if rec.Header().Get("Content-Language") != "fr-CA" || rec.Header().Get("Vary") != "Accept-Language" {
		t.Errorf("headers %v, want Content-Language fr-CA and Vary Accept-Language", rec.Header())
	}
	if got := In(language.English).Language("und"); got != "" {
		t.Errorf("an unsaid language is named %q", got)
	}
}
