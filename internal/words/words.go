// Package words names what the server holds in a reader's language: a track, a copy, a
// resolution. The store and the domain hold values and know no language; the HTTP layers word them
// as they answer, for both APIs, beside the values, so no client composes names of its own and none
// parses these. Every word is a method of Words, so a translation changes this package alone.
package words

import (
	"cmp"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/language"
	"golang.org/x/text/language/display"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// Words names things for one reader. Language names are in the reader's language; the rest is
// English until a translation of it exists.
type Words struct {
	names display.Namer
}

// The languages a reader may read names in; English leads, so one whose languages none of these
// are reads English.
var (
	readable = append([]language.Tag{language.English}, display.Supported.Tags()...)
	matcher  = language.NewMatcher(readable)
)

// Negotiate is the words of a request's reader, by the languages its Accept-Language prefers, and
// says so on the answer: its Content-Language, and that it varies by Accept-Language, so no cache
// hands one reader's words to another.
func Negotiate(w http.ResponseWriter, r *http.Request) Words {
	tags, _, _ := language.ParseAcceptLanguage(r.Header.Get("Accept-Language"))
	_, i, _ := matcher.Match(tags...)
	h := w.Header()
	h.Add("Vary", "Accept-Language")
	h.Set("Content-Language", readable[i].String())
	return In(readable[i])
}

// In is the words of a reader of one language.
func In(tag language.Tag) Words {
	return Words{names: display.Tags(tag)}
}

// The words not yet translated.
const (
	unknown    = "Unknown"
	forced     = "Forced"
	sdh        = "SDH"
	commentary = "Commentary"
	external   = "External"
)

// Language is a language's name by its tag ("fr" is "French", or "Français" to a French reader),
// none for one unsaid, and the tag itself for one it cannot name.
func (w Words) Language(tag string) string {
	if tag == "" || tag == "und" {
		return ""
	}
	t, err := language.Parse(tag)
	if err != nil {
		return tag
	}
	return upperFirst(cmp.Or(w.names.Name(t), tag))
}

// Stream names a track of a copy by what tells it from the others, then how it is made, in
// brackets: "French (Dolby Digital 5.1)". A menu that cuts the name short keeps the part that
// matters. A track with a title of its own leads with it, as that is the name its maker gave it:
// "Director's commentary (English AAC Stereo)".
func (w Words) Stream(s store.StreamPage) string {
	switch s.Kind {
	case domain.StreamVideo:
		picture := []string{codecName(s.Codec), rangeName(s.Range)}
		if s.Title != "" {
			return titled(s.Title, append([]string{Resolution(s.Width)}, picture...))
		}
		return named(Resolution(s.Width), picture)
	case domain.StreamAudio:
		marks := mark(s.Commentary, commentary)
		return w.track(s.Title, s.Language, marks, codecName(s.Codec), channels(s.Channels, s.ChannelLayout))
	case domain.StreamSubtitle:
		marks := append(mark(s.HearingImpaired, sdh), mark(s.Forced, forced)...)
		return w.track(s.Title, s.Language, marks, codecName(s.Codec))
	}
	return s.Title
}

// SubtitleFile names a subtitle file beside a copy, as a subtitle track is named, marked as
// beside it.
func (w Words) SubtitleFile(f store.SubtitleRef) string {
	marks := append(mark(f.HearingImpaired, sdh), mark(f.Forced, forced)...)
	return w.track(f.Title, f.Language, marks, codecName(f.Codec), external)
}

// Version names a copy by its label or edition and its picture ("Director's Cut (4K HEVC Dolby
// Vision)"), or by its picture alone ("4K (HEVC Dolby Vision)"). A label that is the picture's
// own name ("4K") says it once.
func (w Words) Version(v store.VersionPage) string {
	var video store.StreamPage
	for _, s := range v.Streams {
		if s.Kind == domain.StreamVideo {
			video = s
			break
		}
	}
	picture := []string{Resolution(video.Width), codecName(video.Codec), rangeName(video.Range)}
	label := cmp.Or(v.Label, v.Edition)
	if label == "" || strings.EqualFold(label, picture[0]) {
		return named(picture[0], picture[1:])
	}
	return named(label, picture)
}

// track is an audio or subtitle track's name: its title, else its language and marks, then what
// it is in brackets, its language and marks too where its title leads.
func (w Words) track(title, tag string, marks []string, details ...string) string {
	spoken := w.Language(tag)
	if title != "" {
		return titled(title, append(append([]string{spoken}, marks...), details...))
	}
	return named(join(append([]string{cmp.Or(spoken, unknown)}, marks...)), details)
}

// Resolution is how a picture's resolution is spoken of, as a wall's filter files it by width:
// "4K", "1080p", "720p", "SD".
func Resolution(width int) string {
	if width <= 0 {
		return ""
	}
	return resolutions[domain.ResolutionOf(width)]
}

var resolutions = map[domain.Resolution]string{
	domain.ResolutionSD: "SD", domain.ResolutionHD: "720p", domain.ResolutionFHD: "1080p", domain.ResolutionUHD: "4K",
}

// codecName is a codec as people say it: Dolby Digital, not ac3; SRT, not subrip.
func codecName(codec string) string {
	if name, ok := codecs[codec]; ok {
		return name
	}
	return strings.ToUpper(codec)
}

var codecs = map[string]string{
	"ac3": "Dolby Digital", "eac3": "Dolby Digital+", "truehd": "Dolby TrueHD", "dts": "DTS", "dca": "DTS",
	"h264": "H.264", "mpeg2video": "MPEG-2", "mpeg4": "MPEG-4",
	"subrip": "SRT", "webvtt": "WebVTT", "mov_text": "TX3G", "hdmv_pgs_subtitle": "PGS", "dvd_subtitle": "VobSub",
	"dvb_subtitle": "DVB",
}

func rangeName(r domain.Range) string {
	return ranges[r]
}

// SDR is said by saying nothing.
var ranges = map[domain.Range]string{
	domain.RangeHDR10: "HDR10", domain.RangeHDR10Plus: "HDR10+", domain.RangeHLG: "HLG", domain.RangeDV: "Dolby Vision",
}

// channels is a sound's channels as a listing says them: Stereo, 5.1, else its layout or count.
func channels(n int, layout string) string {
	switch n {
	case 1:
		return "Mono"
	case 2:
		return "Stereo"
	case 6:
		return "5.1"
	case 8:
		return "7.1"
	}
	if layout != "" {
		return layout
	}
	if n > 0 {
		return strconv.Itoa(n) + " ch"
	}
	return ""
}

func named(name string, details []string) string {
	rest := join(details)
	switch {
	case name == "":
		return rest
	case rest == "":
		return name
	}
	return name + " (" + rest + ")"
}

// titled is a name led by a track's own title, less the details the title says already.
func titled(title string, details []string) string {
	said := strings.ToLower(title)
	return named(title, slices.DeleteFunc(details, func(d string) bool {
		return d != "" && strings.Contains(said, strings.ToLower(d))
	}))
}

func join(parts []string) string {
	return strings.Join(strings.Fields(strings.Join(parts, " ")), " ")
}

func mark(on bool, word string) []string {
	if on {
		return []string{word}
	}
	return nil
}

func upperFirst(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	return string(unicode.ToUpper(r)) + s[n:]
}
