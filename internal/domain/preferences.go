package domain

import (
	"time"
	"uuid"

	"golang.org/x/text/language"
)

// SubtitleMode is when subtitles come on unasked, as Jellyfin's SubtitlePlaybackMode.
type SubtitleMode string

const (
	// SubtitlesDefault is the file's say: a track it marks default or forced, or a file beside it.
	SubtitlesDefault SubtitleMode = "default"
	// SubtitlesAlways is a full track in the subtitle language, else a forced one.
	SubtitlesAlways SubtitleMode = "always"
	// SubtitlesOnlyForced is a forced track alone, which translates only lines in another language.
	SubtitlesOnlyForced SubtitleMode = "only_forced"
	SubtitlesNone       SubtitleMode = "none"
	// SubtitlesSmart is a track in the subtitle language when the sound is in another, else as
	// SubtitlesOnlyForced.
	SubtitlesSmart SubtitleMode = "smart"
)

func SubtitleModes() []SubtitleMode {
	return []SubtitleMode{SubtitlesDefault, SubtitlesAlways, SubtitlesOnlyForced, SubtitlesNone, SubtitlesSmart}
}

// AudioTrack is which sound plays unasked, as Jellyfin's PlayDefaultAudioTrack.
type AudioTrack string

const (
	// AudioDefault is the track the file marks default, whatever its language.
	AudioDefault AudioTrack = "default"
	// AudioLanguage is a track in the audio language where there is one.
	AudioLanguage AudioTrack = "language"
)

func AudioTracks() []AudioTrack {
	return []AudioTrack{AudioDefault, AudioLanguage}
}

// TrackMemory is whether a title plays again with the tracks last chosen for it, as Jellyfin's
// RememberAudioSelections and RememberSubtitleSelections.
type TrackMemory string

const (
	TrackRemember TrackMemory = "remember"
	TrackForget   TrackMemory = "forget"
)

func TrackMemories() []TrackMemory {
	return []TrackMemory{TrackRemember, TrackForget}
}

// NextEpisode is what a player does with the next episode as one ends, as Jellyfin's
// EnableNextEpisodeAutoPlay.
type NextEpisode string

const (
	NextEpisodePlay  NextEpisode = "play"
	NextEpisodeOffer NextEpisode = "offer"
)

func NextEpisodes() []NextEpisode {
	return []NextEpisode{NextEpisodePlay, NextEpisodeOffer}
}

// SegmentAction is what a player does at a marker, as Jellyfin's MediaSegmentAction.
type SegmentAction string

const (
	SegmentNone SegmentAction = "none"
	// SegmentAsk offers a button to skip it.
	SegmentAsk  SegmentAction = "ask"
	SegmentSkip SegmentAction = "skip"
)

func SegmentActions() []SegmentAction {
	return []SegmentAction{SegmentNone, SegmentAsk, SegmentSkip}
}

// Preferences are how a profile plays, on every device, as Jellyfin's UserConfiguration. The server
// chooses the sound and subtitles by them; the rest are a player's to follow.
type Preferences struct {
	// AudioLanguage and SubtitleLanguage are the languages wanted; Und is any.
	AudioLanguage     language.Tag
	AudioTrack        AudioTrack
	SubtitleLanguage  language.Tag
	SubtitleMode      SubtitleMode
	RememberAudio     TrackMemory
	RememberSubtitles TrackMemory
	// MaxBitrateKbps is the most a player asks the server to send; 0 is the file as it is.
	MaxBitrateKbps int
	NextEpisode    NextEpisode
	// IntroAction is for intros and recaps, CreditsAction for credits and previews.
	IntroAction   SegmentAction
	CreditsAction SegmentAction
	ThemeMusic    ThemeMusic
	// Home is every home row, in the order shown, hidden or not.
	Home []HomeSection
	// SavedAt is when they were last changed; zero for a profile that never has.
	SavedAt time.Time
}

// DefaultPreferences are a profile's before it changes any, Jellyfin's defaults.
func DefaultPreferences() Preferences {
	return Preferences{
		AudioTrack: AudioDefault, SubtitleMode: SubtitlesDefault,
		RememberAudio: TrackRemember, RememberSubtitles: TrackRemember,
		NextEpisode: NextEpisodePlay, IntroAction: SegmentAsk, CreditsAction: SegmentAsk,
		ThemeMusic: ThemeMusicOff, Home: ArrangeHome(nil),
	}
}

// NoSubtitle is subtitles chosen off, as Jellyfin's subtitle index -1.
const NoSubtitle = -1

// ChosenTracks are the sound and subtitles of a title: as a player last chose them, or to play it
// with. Subtitle is a track inside the copy or NoSubtitle, SubtitleFile one beside it; nil is
// nothing said.
type ChosenTracks struct {
	Audio        *int
	Subtitle     *int
	SubtitleFile *uuid.UUID
}
