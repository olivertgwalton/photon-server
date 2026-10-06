package playback

import (
	"testing"
	"uuid"

	"github.com/google/go-cmp/cmp"
	"golang.org/x/text/language"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

var (
	japanese = Track{Stream: 1, Language: language.Japanese, Default: true}
	english  = Track{Stream: 2, Language: language.English}
	comments = Track{Stream: 3, Language: language.English, Commentary: true}

	forcedEnglish = Track{Stream: 4, Language: language.English, Forced: true}
	fullEnglish   = Track{Stream: 5, Language: language.English}
	fullFrench    = Track{Stream: 6, Language: language.French, Default: true}
	srtFile       = Track{File: uuid.MustParse("0199b3c0-0000-7000-8000-0000000000f1"), Language: language.German}
)

func prefs(change func(*domain.Preferences)) domain.Preferences {
	p := domain.DefaultPreferences()
	change(&p)
	return p
}

func TestTheSoundIsTheFilesDefaultOrInTheProfilesLanguage(t *testing.T) {
	audio := []Track{japanese, comments, english}
	tests := []struct {
		name string
		p    domain.Preferences
		last domain.ChosenTracks
		want int
	}{
		{"the file's default by default", domain.DefaultPreferences(), domain.ChosenTracks{}, 1},
		{"the default wins over the language, as Jellyfin's play-default-track does", prefs(func(p *domain.Preferences) {
			p.AudioLanguage = language.English
		}), domain.ChosenTracks{}, 1},
		{"the language, never its commentary", prefs(func(p *domain.Preferences) {
			p.AudioLanguage, p.AudioTrack = language.English, domain.AudioLanguage
		}), domain.ChosenTracks{}, 2},
		{"what was last chosen for the title", domain.DefaultPreferences(), domain.ChosenTracks{Audio: new(3)}, 3},
		{"not where the profile forgets it", prefs(func(p *domain.Preferences) {
			p.RememberAudio = domain.TrackForget
		}), domain.ChosenTracks{Audio: new(3)}, 1},
		{"not a track the copy lacks", domain.DefaultPreferences(), domain.ChosenTracks{Audio: new(9)}, 1},
	}
	for _, tt := range tests {
		if got := DefaultTracks(audio, nil, tt.p, tt.last).Audio; got == nil || *got != tt.want {
			t.Errorf("%s: audio = %v, want %d", tt.name, got, tt.want)
		}
	}
}

func TestSubtitlesComeOnAsTheModeSays(t *testing.T) {
	subs := []Track{forcedEnglish, fullEnglish, fullFrench}
	inEnglish := func(mode domain.SubtitleMode) domain.Preferences {
		return prefs(func(p *domain.Preferences) { p.SubtitleLanguage, p.SubtitleMode = language.English, mode })
	}
	stream := func(n int) domain.ChosenTracks { return domain.ChosenTracks{Audio: new(1), Subtitle: new(n)} }
	none := domain.ChosenTracks{Audio: new(1)}
	tests := []struct {
		name  string
		audio Track
		subs  []Track
		p     domain.Preferences
		last  domain.ChosenTracks
		want  domain.ChosenTracks
	}{
		{"the file's default", japanese, subs, domain.DefaultPreferences(), domain.ChosenTracks{}, stream(6)},
		{
			"a file beside the copy before its own", japanese, append(subs, srtFile), domain.DefaultPreferences(),
			domain.ChosenTracks{},
			domain.ChosenTracks{Audio: new(1), SubtitleFile: &srtFile.File},
		},
		{"never", japanese, subs, inEnglish(domain.SubtitlesNone), domain.ChosenTracks{}, none},
		{"always, a full track", japanese, subs, inEnglish(domain.SubtitlesAlways), domain.ChosenTracks{}, stream(5)},
		{"only forced", japanese, subs, inEnglish(domain.SubtitlesOnlyForced), domain.ChosenTracks{}, stream(4)},
		{"smart, foreign sound: a full track", japanese, subs, inEnglish(domain.SubtitlesSmart), domain.ChosenTracks{}, stream(5)},
		{
			"smart, the reader's own sound: only what is forced", english, subs, inEnglish(domain.SubtitlesSmart),
			domain.ChosenTracks{},
			domain.ChosenTracks{Audio: new(2), Subtitle: new(4)},
		},
		{"smart with no language said: nothing is foreign", japanese, []Track{fullEnglish}, prefs(func(p *domain.Preferences) {
			p.SubtitleMode = domain.SubtitlesSmart
		}), domain.ChosenTracks{}, none},
		{"remembered off", japanese, subs, domain.DefaultPreferences(), domain.ChosenTracks{Subtitle: new(domain.NoSubtitle)}, none},
		{"remembered", japanese, subs, domain.DefaultPreferences(), domain.ChosenTracks{Subtitle: new(5)}, stream(5)},
		{
			"a remembered full track is not forced", japanese, subs, inEnglish(domain.SubtitlesOnlyForced),
			domain.ChosenTracks{Subtitle: new(5)},
			stream(4),
		},
	}
	for _, tt := range tests {
		got := DefaultTracks([]Track{tt.audio}, tt.subs, tt.p, tt.last)
		if diff := cmp.Diff(tt.want, got); diff != "" {
			t.Errorf("%s (-want +got):\n%s", tt.name, diff)
		}
	}
}
