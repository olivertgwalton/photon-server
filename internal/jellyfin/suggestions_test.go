//go:build integration

package jellyfin

import (
	"encoding/json/v2"
	"log/slog"
	"net/http"
	"testing"
	"uuid"
)

// Discrete opens its home with suggestions: albums, picked at random from the libraries the
// profile sees, at either address Jellyfin answers them.
func TestAMusicAppIsSuggestedAlbums(t *testing.T) {
	st, ada, _ := aTrack(t)
	api := New(slog.New(slog.DiscardHandler), uuid.NewV7().String(), func() string { return "Den" }, Services{
		RemoteVersions: noRemoteVersions{}, Discover: noDiscoveries{}, Auth: profiles{"pst_ada": ada}, Catalogue: st, Preferences: st,
	})
	const discrete = `MediaBrowser Client="Discrete", Device="iPhone", DeviceId="1", Version="1.0", Token="pst_ada"`
	for _, target := range []string{
		"/Items/Suggestions?Type=MusicAlbum&MediaType=Audio&Limit=10",
		"/Users/" + guid(ada.ID) + "/Suggestions?type=MusicAlbum&mediaType=Audio&limit=10",
	} {
		w := serve(api, http.MethodGet, target, discrete, "")
		var got struct {
			Items []musicItem
		}
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || w.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", target, w.Code, w.Body)
		}
		if len(got.Items) != 1 || got.Items[0].Name != "Album" || got.Items[0].Type != "MusicAlbum" {
			t.Errorf("%s: %+v, want the one album", target, got.Items)
		}
	}
}
