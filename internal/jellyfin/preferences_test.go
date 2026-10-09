//go:build integration

package jellyfin

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"testing"
	"uuid"

	"golang.org/x/text/language"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/library"
	"github.com/olivertgwalton/photon-server/internal/playback"
)

// defaultSubtitle is the subtitle Infuse plays a title's first copy with unasked for Ada, from its
// PlaybackInfo and from its item: nil for none.
func defaultSubtitle(t *testing.T, api http.Handler, item uuid.UUID) any {
	t.Helper()
	const header = `MediaBrowser Client="Infuse-Direct", Device="Apple TV", DeviceId="E0BE", Version="8.5.6", Token="pst_ada"`
	var info, it struct{ MediaSources []map[string]any }
	for target, into := range map[string]any{"/Items/" + guid(item) + "/PlaybackInfo": &info, "/Items/" + guid(item): &it} {
		w := serve(api, http.MethodGet, target, header, "")
		if err := json.Unmarshal(w.Body.Bytes(), into); err != nil || w.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", target, w.Code, w.Body)
		}
	}
	if len(info.MediaSources) == 0 || len(it.MediaSources) == 0 {
		t.Fatalf("no copies: %v, %v", info.MediaSources, it.MediaSources)
	}
	if a, b := info.MediaSources[0]["DefaultSubtitleStreamIndex"], it.MediaSources[0]["DefaultSubtitleStreamIndex"]; a != b {
		t.Errorf("PlaybackInfo's default subtitle %v, the item's %v", a, b)
	}
	return info.MediaSources[0]["DefaultSubtitleStreamIndex"]
}

// A title's subtitles come on as the profile's preferences say, wherever it set them, as they do
// in photon's own apps.
func TestAnAppsSubtitlesFollowTheProfilesPreferences(t *testing.T) {
	st, ada, heat, _ := aFilm(t)
	api := New(slog.New(slog.DiscardHandler), uuid.NewV7().String(), func() string { return "Den" }, Services{
		Copies: noCopies{}, Discover: noDiscoveries{},
		Sent: playback.NewSent(), Network: st,
		Auth: profiles{"pst_ada": ada}, Catalogue: st, Playing: st, Parts: library.Parts{Places: st}, Watching: st, Preferences: st,
	})
	// A file beside the copy is not the file's say, as a track marked default or forced is.
	if got := defaultSubtitle(t, api, heat); got != nil {
		t.Errorf("by default: subtitle %v, want none", got)
	}
	p := domain.DefaultPreferences()
	p.SubtitleMode, p.SubtitleLanguage = domain.SubtitlesAlways, language.English
	if _, err := st.SetPreferences(t.Context(), ada.ID, p); err != nil {
		t.Fatal(err)
	}
	if got := defaultSubtitle(t, api, heat); got != 2.0 {
		t.Errorf("always, in English: subtitle %v, want 2, the English file beside it", got)
	}
}

// An app saves how its user plays and reads it back, as Jellyfin's web app does from its settings:
// what photon keeps, photon's own apps follow too, and a title's subtitles come on by it; what
// photon keeps no like of, and fields it does not know, are let go. A profile saves no other's.
func TestAnAppSavesItsUsersConfiguration(t *testing.T) {
	ctx := t.Context()
	st, ada, heat, _ := aFilm(t)
	if _, err := st.AddLibrary(ctx, "Shows", domain.LibraryShows, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	bea, err := st.AddProfile(ctx, "Bea", domain.RoleUser, "hash", nil)
	if err != nil {
		t.Fatal(err)
	}
	api := New(slog.New(slog.DiscardHandler), uuid.NewV7().String(), func() string { return "Den" }, Services{
		Copies: noCopies{}, Discover: noDiscoveries{},
		Sent: playback.NewSent(), Network: st,
		Auth: profiles{"pst_ada": ada, "pst_bea": bea}, Catalogue: st, Playing: st, Parts: library.Parts{Places: st}, Watching: st, Preferences: st,
	})
	const web = `MediaBrowser Client="Jellyfin Web", Device="Firefox", DeviceId="TW96", Version="10.11.0", Token="pst_ada"`
	configuration := func() map[string]any {
		t.Helper()
		var u struct{ Configuration map[string]any }
		w := serve(api, http.MethodGet, "/Users/Me", web, "")
		if err := json.Unmarshal(w.Body.Bytes(), &u); err != nil || w.Code != http.StatusOK {
			t.Fatalf("/Users/Me: %d %s", w.Code, w.Body)
		}
		return u.Configuration
	}
	save := func(target, body string, want int) {
		t.Helper()
		if w := serve(api, http.MethodPost, target, web, body); w.Code != want {
			t.Fatalf("POST %s: %d %s, want %d", target, w.Code, w.Body, want)
		}
	}

	c := configuration()
	views, _ := c["OrderedViews"].([]any)
	if len(views) != 2 || c["SubtitleMode"] != "Default" || c["SubtitleLanguagePreference"] != "" {
		t.Fatalf("a new user's configuration: %v", c)
	}
	c["AudioLanguagePreference"], c["SubtitleLanguagePreference"], c["SubtitleMode"] = "fre", "eng", "Always"
	c["PlayDefaultAudioTrack"], c["RememberSubtitleSelections"], c["EnableNextEpisodeAutoPlay"] = false, false, false
	c["OrderedViews"] = []any{views[1], "0123456789abcdef0123456789abcdef", views[0]}
	c["HidePlayedInLatest"], c["ChromecastVersion"] = false, "Stable"
	body, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	save("/Users/Configuration?userId="+guid(ada.ID), string(body), http.StatusNoContent)

	c = configuration()
	for k, want := range map[string]any{
		"AudioLanguagePreference": "fra", "SubtitleLanguagePreference": "eng", "SubtitleMode": "Always",
		"PlayDefaultAudioTrack": false, "RememberAudioSelections": true, "RememberSubtitleSelections": false,
		"EnableNextEpisodeAutoPlay": false, "HidePlayedInLatest": true,
	} {
		if c[k] != want {
			t.Errorf("%s read back as %v, want %v", k, c[k], want)
		}
	}
	if got, _ := c["OrderedViews"].([]any); len(got) != 2 || got[0] != views[1] || got[1] != views[0] {
		t.Errorf("libraries ordered %v, want %v reversed", got, views)
	}
	if _, ok := c["ChromecastVersion"]; ok {
		t.Errorf("a field photon does not know was kept: %v", c)
	}
	p, err := st.Preferences(ctx, ada.ID)
	if err != nil || p.SubtitleMode != domain.SubtitlesAlways || p.SubtitleLanguage.String() != "en" || p.AudioTrack != domain.AudioLanguage {
		t.Errorf("photon's own apps see %+v, %v", p, err)
	}
	if got := defaultSubtitle(t, api, heat); got != 2.0 {
		t.Errorf("English subtitles always: subtitle %v, want 2, the English file", got)
	}

	// The legacy route, and a body of one field, which leaves the rest as they were.
	save("/Users/"+guid(ada.ID)+"/Configuration", `{"SubtitleMode":"OnlyForced"}`, http.StatusNoContent)
	if c = configuration(); c["SubtitleMode"] != "OnlyForced" || c["SubtitleLanguagePreference"] != "eng" {
		t.Errorf("after saving one field: %v", c)
	}
	if got := defaultSubtitle(t, api, heat); got != nil {
		t.Errorf("forced subtitles alone: subtitle %v, want none", got)
	}

	save("/Users/Configuration", `{"SubtitleMode":"Sometimes"}`, http.StatusBadRequest)
	save("/Users/Configuration", `{"SubtitleLanguagePreference":"not a language"}`, http.StatusBadRequest)
	save("/Users/Configuration?userId="+guid(bea.ID), `{"SubtitleMode":"None"}`, http.StatusForbidden)
	save("/Users/"+guid(bea.ID)+"/Configuration", `{"SubtitleMode":"None"}`, http.StatusForbidden)
	if p, err := st.Preferences(ctx, bea.ID); err != nil || p.SubtitleMode != domain.SubtitlesDefault {
		t.Errorf("another profile's preferences: %+v, %v", p, err)
	}
}
