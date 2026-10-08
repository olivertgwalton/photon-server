package jellyfin

import (
	"cmp"
	"context"
	"net/http"
	"slices"
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

// configuration is Jellyfin's UserConfiguration: a profile's preferences and the order of its
// libraries. An app that decodes one needs every field: the Kotlin SDK refuses one short of any.
// What photon keeps no like of is answered as Jellyfin has it for a new user, and let go when an
// app saves it.
type configuration struct {
	AudioLanguagePreference    *string  `json:"AudioLanguagePreference"`
	PlayDefaultAudioTrack      bool     `json:"PlayDefaultAudioTrack"`
	SubtitleLanguagePreference *string  `json:"SubtitleLanguagePreference"`
	DisplayMissingEpisodes     bool     `json:"DisplayMissingEpisodes"`
	GroupedFolders             []string `json:"GroupedFolders"`
	SubtitleMode               string   `json:"SubtitleMode"`
	DisplayCollectionsView     bool     `json:"DisplayCollectionsView"`
	EnableLocalPassword        bool     `json:"EnableLocalPassword"`
	OrderedViews               []string `json:"OrderedViews"`
	LatestItemsExcludes        []string `json:"LatestItemsExcludes"`
	MyMediaExcludes            []string `json:"MyMediaExcludes"`
	HidePlayedInLatest         bool     `json:"HidePlayedInLatest"`
	RememberAudioSelections    bool     `json:"RememberAudioSelections"`
	RememberSubtitleSelections bool     `json:"RememberSubtitleSelections"`
	EnableNextEpisodeAutoPlay  bool     `json:"EnableNextEpisodeAutoPlay"`
	CastReceiverID             string   `json:"CastReceiverId"`
}

// subtitleModes are Jellyfin's SubtitlePlaybackMode for each of photon's, which were made after them.
var subtitleModes = map[domain.SubtitleMode]string{
	domain.SubtitlesDefault: "Default", domain.SubtitlesAlways: "Always", domain.SubtitlesOnlyForced: "OnlyForced",
	domain.SubtitlesNone: "None", domain.SubtitlesSmart: "Smart",
}

func configurationOf(p domain.Preferences, libs []*store.SeenLibrary) configuration {
	audio, subtitles := iso639(domain.TagOf(p.AudioLanguage)), iso639(domain.TagOf(p.SubtitleLanguage))
	c := configuration{
		AudioLanguagePreference: &audio, PlayDefaultAudioTrack: p.AudioTrack == domain.AudioDefault,
		SubtitleLanguagePreference: &subtitles, SubtitleMode: subtitleModes[p.SubtitleMode],
		GroupedFolders: []string{}, OrderedViews: make([]string, len(libs)), LatestItemsExcludes: []string{},
		MyMediaExcludes: []string{}, HidePlayedInLatest: true,
		RememberAudioSelections:    p.RememberAudio == domain.TrackRemember,
		RememberSubtitleSelections: p.RememberSubtitles == domain.TrackRemember,
		EnableNextEpisodeAutoPlay:  p.NextEpisode == domain.NextEpisodePlay,
		CastReceiverID:             "F007D354",
	}
	for n, l := range libs {
		c.OrderedViews[n] = guid(l.ID)
	}
	return c
}

// onto is the profile's preferences p as c changes them, or false for a language or mode photon
// cannot read.
func (c configuration) onto(p domain.Preferences) (domain.Preferences, bool) {
	audio, audioOK := tagOf(c.AudioLanguagePreference)
	subtitles, subtitlesOK := tagOf(c.SubtitleLanguagePreference)
	mode := domain.SubtitleMode("")
	for m, name := range subtitleModes {
		if name == c.SubtitleMode {
			mode = m
		}
	}
	p.AudioLanguage, p.SubtitleLanguage, p.SubtitleMode = audio, subtitles, mode
	p.AudioTrack = pick(c.PlayDefaultAudioTrack, domain.AudioDefault, domain.AudioLanguage)
	p.RememberAudio = pick(c.RememberAudioSelections, domain.TrackRemember, domain.TrackForget)
	p.RememberSubtitles = pick(c.RememberSubtitleSelections, domain.TrackRemember, domain.TrackForget)
	p.NextEpisode = pick(c.EnableNextEpisodeAutoPlay, domain.NextEpisodePlay, domain.NextEpisodeOffer)
	return p, audioOK && subtitlesOK && mode != ""
}

// tagOf reads a language as Jellyfin's apps write one, by its ISO 639-2 letters, either set: "fre"
// is "fra". Null and empty are any.
func tagOf(s *string) (language.Tag, bool) {
	if s == nil || *s == "" {
		return language.Und, true
	}
	t, err := language.Parse(*s)
	return t, err == nil
}

func pick[T any](yes bool, a, b T) T {
	if yes {
		return a
	}
	return b
}

// configured answers the profile's preferences and the libraries it sees, in its order.
func (a *API) configured(ctx context.Context, profile uuid.UUID) (domain.Preferences, []*store.SeenLibrary, error) {
	p, err := a.svc.Preferences.Preferences(ctx, profile)
	if err != nil {
		return p, nil, err
	}
	libs, err := a.svc.Catalogue.LibrariesSeen(ctx, profile)
	return p, libs, err
}

// setConfiguration keeps what of a user's configuration photon has a like of, where photon's own
// apps keep it, so they follow it too. A profile changes its own and no other's, as Jellyfin
// refuses a user who is not an administrator, which none is here. A view that is none of the
// profile's libraries is let go from its order.
func (a *API) setConfiguration(w http.ResponseWriter, r *http.Request) {
	profile := auth.SessionOf(r.Context()).Profile.ID
	if userID := cmp.Or(r.PathValue("userId"), query(r, "userId")); userID != "" {
		if id, err := uuid.Parse(userID); err != nil || id != profile {
			a.refuse(w, http.StatusForbidden)
			return
		}
	}
	p, libs, err := a.configured(r.Context(), profile)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	c := configurationOf(p, libs)
	was := slices.Clone(c.OrderedViews)
	if !a.readJSON(w, r, &c) {
		return
	}
	p, ok := c.onto(p)
	if !ok {
		a.refuse(w, http.StatusBadRequest)
		return
	}
	if _, err := a.svc.Preferences.SetPreferences(r.Context(), profile, p); err != nil {
		a.internal(w, r, err)
		return
	}
	if !slices.Equal(c.OrderedViews, was) {
		var order []uuid.UUID
		for _, v := range c.OrderedViews {
			if id, ok := parseID(v); ok && slices.ContainsFunc(libs, func(l *store.SeenLibrary) bool { return l.ID == id }) && !slices.Contains(order, id) {
				order = append(order, id)
			}
		}
		if err := a.svc.Catalogue.SetLibraryOrder(r.Context(), profile, order); err != nil {
			a.internal(w, r, err)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// chooseTracks says the tracks each copy of a title plays with for a profile, by the preferences
// and choices photon's own apps play by, as Jellyfin's media sources answer their defaults.
func (a *API) chooseTracks(ctx context.Context, profile, item uuid.UUID, versions []store.VersionPage) error {
	if len(versions) == 0 {
		return nil
	}
	prefs, err := a.svc.Preferences.Preferences(ctx, profile)
	if err != nil {
		return err
	}
	last, err := a.svc.Preferences.ChosenTracks(ctx, profile, item)
	if err != nil {
		return err
	}
	playback.ChooseTracks(versions, prefs, last)
	return nil
}
