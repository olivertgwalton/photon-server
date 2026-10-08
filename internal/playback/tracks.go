package playback

import (
	"cmp"
	"slices"
	"uuid"

	"golang.org/x/text/language"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// Track is a sound or subtitle track to choose from: one inside the copy by its stream index, or
// a subtitle file beside it by its id.
type Track struct {
	Stream     int
	File       uuid.UUID
	Language   language.Tag
	Default    bool
	Forced     bool
	Commentary bool
}

// DefaultTracks are the sound and subtitles a profile's copy plays with unasked, as Jellyfin's
// SetDefaultAudioAndSubtitleStreamIndices chooses them: what was last chosen for the title where
// the profile keeps that, else what its preferences ask of the tracks there are. No subtitle is
// answered as none chosen, never as domain.NoSubtitle.
func DefaultTracks(audio, subtitles []Track, p domain.Preferences, last domain.ChosenTracks) domain.ChosenTracks {
	var out domain.ChosenTracks
	sound := defaultAudio(audio, p, last)
	soundLanguage := language.Und
	if sound != nil {
		out.Audio = &sound.Stream
		soundLanguage = sound.Language
	}
	if s := defaultSubtitle(subtitles, soundLanguage, p, last); s != nil {
		if s.File != (uuid.UUID{}) {
			out.SubtitleFile = &s.File
		} else {
			out.Subtitle = &s.Stream
		}
	}
	return out
}

// ChooseTracks says the tracks each copy plays with unasked for a profile, by DefaultTracks.
func ChooseTracks(versions []store.VersionPage, p domain.Preferences, last domain.ChosenTracks) {
	for i, v := range versions {
		audio, subtitles := pageTracks(v)
		d := DefaultTracks(audio, subtitles, p, last)
		versions[i].DefaultAudioStream, versions[i].DefaultSubtitleStream, versions[i].DefaultSubtitleFile = d.Audio, d.Subtitle, d.SubtitleFile
	}
}

// pageTracks are a copy's sound and subtitles as its page lists them.
func pageTracks(v store.VersionPage) (audio, subtitles []Track) {
	for _, s := range v.Streams {
		l := language.Make(s.Language)
		t := Track{Stream: s.Index, Language: l, Default: s.Default, Forced: s.Forced, Commentary: s.Commentary}
		switch s.Kind {
		case domain.StreamAudio:
			audio = append(audio, t)
		case domain.StreamSubtitle:
			subtitles = append(subtitles, t)
		case domain.StreamVideo:
		}
	}
	for _, f := range v.Subtitles {
		l := language.Make(f.Language)
		subtitles = append(subtitles, Track{File: f.ID, Language: l, Default: f.Default, Forced: f.Forced})
	}
	return audio, subtitles
}

func defaultAudio(audio []Track, p domain.Preferences, last domain.ChosenTracks) *Track {
	if last.Audio != nil && p.RememberAudio == domain.TrackRemember {
		if i := slices.IndexFunc(audio, func(t Track) bool { return t.Stream == *last.Audio }); i >= 0 {
			return &audio[i]
		}
	}
	if len(audio) == 0 {
		return nil
	}
	sorted := slices.Clone(audio)
	slices.SortStableFunc(sorted, func(a, b Track) int {
		return cmp.Or(
			first(matches(a.Language, p.AudioLanguage), matches(b.Language, p.AudioLanguage)),
			first(!a.Commentary, !b.Commentary),
			first(a.Default, b.Default),
		)
	})
	switch p.AudioTrack {
	case domain.AudioDefault:
		if i := slices.IndexFunc(sorted, func(t Track) bool { return t.Default }); i >= 0 {
			return &sorted[i]
		}
	case domain.AudioLanguage:
	}
	return &sorted[0]
}

func defaultSubtitle(subs []Track, sound language.Tag, p domain.Preferences, last domain.ChosenTracks) *Track {
	if p.SubtitleMode == domain.SubtitlesNone {
		return nil
	}
	if p.RememberSubtitles == domain.TrackRemember {
		if last.Subtitle != nil && *last.Subtitle == domain.NoSubtitle {
			return nil
		}
		if i := slices.IndexFunc(subs, func(t Track) bool { return t.chosen(last) }); i >= 0 &&
			(p.SubtitleMode != domain.SubtitlesOnlyForced || subs[i].Forced) {
			return &subs[i]
		}
	}
	// Unlike Jellyfin, a file beside the copy comes after the copy's own tracks as good: one fetched
	// or left by another tool is often timed for another release, and a text track inside the copy
	// plays as cheaply as one beside it.
	want := p.SubtitleLanguage
	sorted := slices.Clone(subs)
	slices.SortStableFunc(sorted, func(a, b Track) int {
		return cmp.Or(
			first(a.Default, b.Default),
			first(!a.Forced && matches(a.Language, want), !b.Forced && matches(b.Language, want)),
			first(a.Forced && matches(a.Language, want), b.Forced && matches(b.Language, want)),
			first(a.Forced && a.Language == language.Und, b.Forced && b.Language == language.Und),
			first(a.Forced, b.Forced),
			first(a.File == uuid.UUID{}, b.File == uuid.UUID{}),
		)
	})
	find := func(ok func(Track) bool) *Track {
		if i := slices.IndexFunc(sorted, ok); i >= 0 {
			return &sorted[i]
		}
		return nil
	}
	onlyForced := func() *Track {
		if t := find(func(t Track) bool { return t.Forced && matches(t.Language, want) }); t != nil {
			return t
		}
		return find(func(t Track) bool { return t.Forced && t.Language == language.Und })
	}
	switch p.SubtitleMode {
	case domain.SubtitlesDefault:
		return find(func(t Track) bool { return t.Default || t.Forced })
	case domain.SubtitlesAlways:
		if t := find(func(t Track) bool { return !t.Forced && matches(t.Language, want) }); t != nil {
			return t
		}
		return onlyForced()
	case domain.SubtitlesOnlyForced:
		return onlyForced()
	case domain.SubtitlesSmart:
		// The reader's own language is the one subtitles are wanted in, else the one sound is; with
		// neither said, nothing is foreign.
		own := cmp.Or(want, p.AudioLanguage)
		if own != language.Und && sound != language.Und && !matches(sound, own) {
			return find(func(t Track) bool { return matches(t.Language, want) })
		}
		return onlyForced()
	case domain.SubtitlesNone:
	}
	return nil
}

func (t Track) chosen(last domain.ChosenTracks) bool {
	if t.File != (uuid.UUID{}) {
		return last.SubtitleFile != nil && *last.SubtitleFile == t.File
	}
	return last.Subtitle != nil && *last.Subtitle == t.Stream
}

// matches is whether a track is in a language wanted: any, where none is said.
func matches(have, want language.Tag) bool {
	if want == language.Und {
		return true
	}
	h, _ := have.Base()
	w, _ := want.Base()
	return have != language.Und && h == w
}

// first orders what a has before what it lacks.
func first(a, b bool) int {
	switch {
	case a == b:
		return 0
	case a:
		return -1
	}
	return 1
}
