//go:build integration

package jellyfin

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"testing"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
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
	api := New(slog.New(slog.DiscardHandler), domain.Info{ID: uuid.NewV7().String(), Name: "Den"}, Services{
		Sent: playback.NewSent(), Network: st,
		Auth: profiles{"pst_ada": ada}, Catalogue: st, Playing: st, Watching: st, Preferences: st,
	})
	if got := defaultSubtitle(t, api, heat); got != 2.0 {
		t.Errorf("by default: subtitle %v, want 2, the file beside it", got)
	}
	p := domain.DefaultPreferences()
	p.SubtitleMode = domain.SubtitlesNone
	if _, err := st.SetPreferences(t.Context(), ada.ID, p); err != nil {
		t.Fatal(err)
	}
	if got := defaultSubtitle(t, api, heat); got != nil {
		t.Errorf("with subtitles off: subtitle %v, want none", got)
	}
}
