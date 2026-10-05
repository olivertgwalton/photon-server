package naming

import (
	"path"
	"slices"
	"strings"
	"sync"

	"golang.org/x/text/language"
	"golang.org/x/text/language/display"
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
		case s.Language == language.Und && languageOf(seg) != language.Und:
			s.Language = languageOf(seg)
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

// languageOf reads a tag as a language: a two- or three-letter code, or a language's English
// name as releases name their subtitle files ("English", "2_English"). Longer codes are not
// tried, so a title word that happens to be a registered subtag is not read as one.
func languageOf(s string) language.Tag {
	if len(s) == 2 || len(s) == 3 {
		if t, err := language.Parse(s); err == nil {
			return t
		}
	}
	name := strings.ToLower(strings.TrimLeft(s, "0123456789_ -"))
	return languageNames()[name]
}

// languageNames maps each language's English name to its tag, from x/text's own names.
var languageNames = sync.OnceValue(func() map[string]language.Tag {
	names := map[string]language.Tag{}
	namer := display.English.Languages()
	for _, t := range display.Supported.Tags() {
		base, _ := t.Base()
		tag := language.Make(base.String())
		if n := namer.Name(tag); n != "" {
			names[strings.ToLower(n)] = tag
		}
	}
	return names
})

// subtitleCodecs name a subtitle file's format as ffprobe names an embedded track's, so a client
// treats the two alike. A .sub is read through its .idx, and a .mks is a container ffprobe would
// have to open, so neither is listed.
var subtitleCodecs = map[string]string{
	".srt": "subrip", ".ass": "ass", ".ssa": "ssa", ".vtt": "webvtt",
	".sup": "hdmv_pgs_subtitle", ".idx": "dvd_subtitle", ".smi": "sami", ".sami": "sami",
}

// SubtitleCodec is the format of a subtitle file, by its extension.
func SubtitleCodec(name string) (string, bool) {
	c, ok := subtitleCodecs[strings.ToLower(path.Ext(name))]
	return c, ok
}
