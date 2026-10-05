package naming

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"golang.org/x/text/language"
)

func TestSubtitleTags(t *testing.T) {
	tests := []struct {
		tags string
		want Subtitle
	}{
		{".en.forced", Subtitle{Language: language.English, Forced: true}},
		{".title.en.default.forced", Subtitle{Language: language.English, Title: "title", Default: true, Forced: true}},
		{".hi.en.title", Subtitle{Language: language.English, Title: "title", HearingImpaired: true}},
		{".hi", Subtitle{Language: language.Hindi}},
		{".en.fr", Subtitle{Language: language.French, Title: "en"}},
		{".en.sdh", Subtitle{Language: language.English, HearingImpaired: true}},
		{".eng.cc", Subtitle{Language: language.English, HearingImpaired: true}},
		{".Subs for Chinese Audio.eng", Subtitle{Language: language.English, Title: "Subs for Chinese Audio"}},
		{".foreign", Subtitle{Forced: true}},
		{".English", Subtitle{Language: language.English}},
		{".2_English", Subtitle{Language: language.English}},
		{".french.forced", Subtitle{Language: language.French, Forced: true}},
		{".Commentary", Subtitle{Title: "Commentary"}},
		{"", Subtitle{}},
	}
	tagString := cmp.Transformer("tag", func(t language.Tag) string { return t.String() })
	for _, tt := range tests {
		t.Run(tt.tags, func(t *testing.T) {
			if diff := cmp.Diff(tt.want, SubtitleTags(tt.tags), tagString); diff != "" {
				t.Errorf("SubtitleTags(%q) (-want +got):\n%s", tt.tags, diff)
			}
		})
	}
}

func TestSubtitleCodec(t *testing.T) {
	for name, want := range map[string]string{
		"Heat.en.srt": "subrip", "Heat.ASS": "ass", "Heat.vtt": "webvtt", "Heat.sup": "hdmv_pgs_subtitle",
		"Heat.idx": "dvd_subtitle", "Heat.sub": "", "Heat.mks": "", "Heat.nfo": "",
	} {
		got, ok := SubtitleCodec(name)
		if got != want || ok != (want != "") {
			t.Errorf("SubtitleCodec(%q) = %q, %v; want %q", name, got, ok, want)
		}
	}
}
