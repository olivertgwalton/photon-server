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
