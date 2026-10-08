package httpapi

import (
	"cmp"
	"context"
	"net/http"
	"time"
	"uuid"

	"golang.org/x/text/language"

	"github.com/olivertgwalton/photon-server/internal/auth"
	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/playback"
	"github.com/olivertgwalton/photon-server/internal/store"
)

type preferences interface {
	Preferences(ctx context.Context, profile uuid.UUID) (domain.Preferences, error)
	SetPreferences(ctx context.Context, profile uuid.UUID, p domain.Preferences) (domain.Preferences, error)
	ChosenTracks(ctx context.Context, profile, item uuid.UUID) (domain.ChosenTracks, error)
}

// preferencesJSON is how a profile plays. The server chooses a title's sound and subtitles by
// them; the rest are each player's to follow.
type preferencesJSON struct {
	// AudioLanguage and SubtitleLanguage are BCP 47 tags; empty is any.
	AudioLanguage     string               `json:"audio_language"`
	AudioTrack        domain.AudioTrack    `json:"audio_track"`
	SubtitleLanguage  string               `json:"subtitle_language"`
	SubtitleMode      domain.SubtitleMode  `json:"subtitle_mode"`
	RememberAudio     domain.TrackMemory   `json:"remember_audio"`
	RememberSubtitles domain.TrackMemory   `json:"remember_subtitles"`
	MaxBitrateKbps    int                  `json:"max_bitrate_kbps"`
	NextEpisode       domain.NextEpisode   `json:"next_episode"`
	IntroAction       domain.SegmentAction `json:"intro_action"`
	CreditsAction     domain.SegmentAction `json:"credits_action"`
	// ThemeMusic is whether a title's page plays its theme tune.
	ThemeMusic domain.ThemeMusic `json:"theme_music"`
	// Home is every row of the profile's home, in the order /api/v1/home answers them.
	Home []homeSectionJSON `json:"home"`
	// SavedAt is when they were last changed: absent while every one is its default.
	SavedAt *time.Time `json:"saved_at,omitzero"`
}

// preferencesChangeJSON is the preferences to change; one left out stays as it is.
type preferencesChangeJSON struct {
	AudioLanguage     *string              `json:"audio_language,omitzero"`
	AudioTrack        domain.AudioTrack    `json:"audio_track,omitzero"`
	SubtitleLanguage  *string              `json:"subtitle_language,omitzero"`
	SubtitleMode      domain.SubtitleMode  `json:"subtitle_mode,omitzero"`
	RememberAudio     domain.TrackMemory   `json:"remember_audio,omitzero"`
	RememberSubtitles domain.TrackMemory   `json:"remember_subtitles,omitzero"`
	MaxBitrateKbps    *int                 `json:"max_bitrate_kbps,omitzero"`
	NextEpisode       domain.NextEpisode   `json:"next_episode,omitzero"`
	IntroAction       domain.SegmentAction `json:"intro_action,omitzero"`
	CreditsAction     domain.SegmentAction `json:"credits_action,omitzero"`
	ThemeMusic        domain.ThemeMusic    `json:"theme_music,omitzero"`
	// Home is the rows in the order wanted, each once; any left out follow, shown.
	Home []homeSectionJSON `json:"home,omitzero"`
}

type homeSectionJSON struct {
	Row        domain.HomeRow       `json:"row"`
	Visibility domain.RowVisibility `json:"visibility"`
}

func preferencesOf(p domain.Preferences) preferencesJSON {
	out := preferencesJSON{
		AudioLanguage: domain.TagOf(p.AudioLanguage), AudioTrack: p.AudioTrack,
		SubtitleLanguage: domain.TagOf(p.SubtitleLanguage), SubtitleMode: p.SubtitleMode,
		RememberAudio: p.RememberAudio, RememberSubtitles: p.RememberSubtitles,
		MaxBitrateKbps: p.MaxBitrateKbps, NextEpisode: p.NextEpisode,
		IntroAction: p.IntroAction, CreditsAction: p.CreditsAction, ThemeMusic: p.ThemeMusic,
		Home: make([]homeSectionJSON, len(p.Home)),
	}
	for i, h := range p.Home {
		out.Home[i] = homeSectionJSON(h)
	}
	if !p.SavedAt.IsZero() {
		out.SavedAt = &p.SavedAt
	}
	return out
}

// ownPreferences answers how the profile plays, as Jellyfin's user configuration.
func (a *API) ownPreferences(w http.ResponseWriter, r *http.Request) {
	p, err := a.svc.Preferences.Preferences(r.Context(), auth.SessionOf(r.Context()).Profile.ID)
	if !a.answered(w, r, err) {
		writeJSON(w, a.logger, "application/json", http.StatusOK, preferencesOf(p))
	}
}

// setOwnPreferences changes how the profile plays, on every device it plays on.
func (a *API) setOwnPreferences(w http.ResponseWriter, r *http.Request) {
	var req preferencesChangeJSON
	if !a.decode(w, r, &req) {
		return
	}
	profile := auth.SessionOf(r.Context()).Profile.ID
	p, err := a.svc.Preferences.Preferences(r.Context(), profile)
	if a.answered(w, r, err) {
		return
	}
	for _, l := range []struct {
		set  *string
		into *language.Tag
	}{{req.AudioLanguage, &p.AudioLanguage}, {req.SubtitleLanguage, &p.SubtitleLanguage}} {
		if l.set == nil {
			continue
		}
		if *l.into, err = language.Parse(*l.set); *l.set != "" && err != nil {
			writeProblem(w, a.logger, codeInvalidBody, "audio_language and subtitle_language are BCP 47 tags, or empty for any")
			return
		}
	}
	if req.MaxBitrateKbps != nil {
		if *req.MaxBitrateKbps < 0 {
			writeProblem(w, a.logger, codeInvalidBody, "max_bitrate_kbps is not negative; 0 is the file as it is")
			return
		}
		p.MaxBitrateKbps = *req.MaxBitrateKbps
	}
	p.AudioTrack, p.SubtitleMode = cmp.Or(req.AudioTrack, p.AudioTrack), cmp.Or(req.SubtitleMode, p.SubtitleMode)
	p.RememberAudio = cmp.Or(req.RememberAudio, p.RememberAudio)
	p.RememberSubtitles = cmp.Or(req.RememberSubtitles, p.RememberSubtitles)
	p.NextEpisode = cmp.Or(req.NextEpisode, p.NextEpisode)
	p.IntroAction, p.CreditsAction = cmp.Or(req.IntroAction, p.IntroAction), cmp.Or(req.CreditsAction, p.CreditsAction)
	p.ThemeMusic = cmp.Or(req.ThemeMusic, p.ThemeMusic)
	if req.Home != nil {
		seen := map[domain.HomeRow]bool{}
		p.Home = make([]domain.HomeSection, len(req.Home))
		for i, h := range req.Home {
			if h.Row == "" || h.Visibility == "" || seen[h.Row] {
				writeProblem(w, a.logger, codeInvalidBody, "home is rows, each once, with their visibility")
				return
			}
			seen[h.Row] = true
			p.Home[i] = domain.HomeSection(h)
		}
	}
	p, err = a.svc.Preferences.SetPreferences(r.Context(), profile, p)
	if !a.answered(w, r, err) {
		writeJSON(w, a.logger, "application/json", http.StatusOK, preferencesOf(p))
	}
}

// playingAs answers a profile's preferences and the tracks it last chose for a title, to choose the
// title's tracks by.
func (a *API) playingAs(ctx context.Context, profile, item uuid.UUID) (domain.Preferences, domain.ChosenTracks, error) {
	p, err := a.svc.Preferences.Preferences(ctx, profile)
	if err != nil {
		return p, domain.ChosenTracks{}, err
	}
	last, err := a.svc.Preferences.ChosenTracks(ctx, profile, item)
	return p, last, err
}

// chooseTracks says the tracks each copy of a film or episode plays with for a profile, as
// Jellyfin's item answers its media sources' defaults.
func (a *API) chooseTracks(ctx context.Context, profile uuid.UUID, page *store.TitlePage) error {
	if len(page.Versions) == 0 {
		return nil
	}
	prefs, last, err := a.playingAs(ctx, profile, page.ID)
	if err != nil {
		return err
	}
	for i, v := range page.Versions {
		audio, subtitles := pageTracks(v)
		d := playback.DefaultTracks(audio, subtitles, prefs, last)
		page.Versions[i].DefaultAudioStream, page.Versions[i].DefaultSubtitleStream = d.Audio, d.Subtitle
		page.Versions[i].DefaultSubtitleFile = d.SubtitleFile
	}
	return nil
}

// pageTracks are a copy's sound and subtitles as its page lists them.
func pageTracks(v store.VersionPage) (audio, subtitles []playback.Track) {
	for _, s := range v.Streams {
		l := language.Make(s.Language)
		t := playback.Track{Stream: s.Index, Language: l, Default: s.Default, Forced: s.Forced, Commentary: s.Commentary}
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
		subtitles = append(subtitles, playback.Track{File: f.ID, Language: l, Default: f.Default, Forced: f.Forced})
	}
	return audio, subtitles
}

// copyTracks are a copy's sound and subtitles as it is played.
func copyTracks(c store.PlayCopy) (audio, subtitles []playback.Track) {
	for _, s := range c.Streams {
		t := playback.Track{Stream: s.Index, Language: s.Language, Default: s.Default, Forced: s.Forced, Commentary: s.Commentary}
		switch s.Kind {
		case domain.StreamAudio:
			audio = append(audio, t)
		case domain.StreamSubtitle:
			subtitles = append(subtitles, t)
		case domain.StreamVideo:
		}
	}
	for _, f := range c.Subtitles {
		subtitles = append(subtitles, playback.Track{File: f.ID, Language: f.Language, Default: f.Default, Forced: f.Forced})
	}
	return audio, subtitles
}

func (a *API) preferencesRoutes() []route {
	return []route{
		{
			pattern: "GET /api/v1/me/preferences", access: signedIn,
			summary: "How the profile plays on every device: the server's defaults until it changes them",
			status:  http.StatusOK, reply: preferencesJSON{}, handle: a.ownPreferences,
		},
		{
			pattern: "PATCH /api/v1/me/preferences", access: signedIn,
			summary: "Change how the profile plays on every device; what is left out stays",
			body:    preferencesChangeJSON{}, status: http.StatusOK, reply: preferencesJSON{}, handle: a.setOwnPreferences,
		},
	}
}
