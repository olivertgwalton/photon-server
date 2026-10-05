package naming

import (
	"slices"
	"strings"

	"golang.org/x/text/language"
)

// Subtitle is what the tags between a video's name and a subtitle's extension say about it:
// "Movie.en.forced.srt", "Movie.title.en.default.sdh.srt".
type Subtitle struct {
	Language        language.Tag
	Title           string
	Forced          bool
	Default         bool
	HearingImpaired bool
}

// SubtitleTags reads tags (".en.forced") from the right. "hi" means hearing impaired beside
// another language and Hindi on its own; sdh and cc are checked before languages because both are
// also ISO 639-3 codes.
func SubtitleTags(tags string) Subtitle {
	var s Subtitle
	var title []string
	hi := false
	segs := strings.Split(strings.Trim(tags, "."), ".")
	for _, seg := range slices.Backward(segs) {
		lower := strings.ToLower(seg)
		switch {
		case seg == "":
		case strings.Contains(lower, "default"):
			s.Default = true
		case strings.Contains(lower, "forced"), strings.Contains(lower, "foreign"):
			s.Forced = true
		case lower == "sdh", lower == "cc":
			s.HearingImpaired = true
		case lower == "hi":
			hi = true
		case s.Language == language.Und && isLanguage(seg):
			s.Language, _ = language.Parse(seg)
		default:
			title = append(title, seg)
		}
	}
	if hi {
		if s.Language == language.Und {
			s.Language = language.Hindi
		} else {
			s.HearingImpaired = true
		}
	}
	slices.Reverse(title)
	s.Title = strings.Join(title, ".")
	return s
}

// Only two- and three-letter codes count, so a title word that happens to be a registered
// five-letter subtag is not read as a language.
func isLanguage(s string) bool {
	if len(s) != 2 && len(s) != 3 {
		return false
	}
	_, err := language.Parse(s)
	return err == nil
}
