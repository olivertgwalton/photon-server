package domain

import "golang.org/x/text/language"

// SubtitleQuery is what subtitles are searched for, as OpenSubtitles asks: a film by its ids, or an
// episode by its show's ids and its numbers; the release by its file's hash where it has one; in
// a language.
type SubtitleQuery struct {
	Kind            ItemKind
	IDs             map[Provider]string
	Season, Episode int
	Hash            string
	Language        language.Tag
}

// FoundSubtitle is a subtitle a provider has for a title: its id there, what it is, and whether
// it was made for the release searched for, which Plex stars.
type FoundSubtitle struct {
	Source          FieldSource
	ID              string
	Language        language.Tag
	Release         string
	HearingImpaired bool
	Forced          bool
	ForRelease      bool
	Downloads       int
}
