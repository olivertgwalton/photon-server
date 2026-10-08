//go:build integration

package jellyfin

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"testing"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// An app reads a film's trickplay, as Jellyfin's web app and Swiftfin do to show a thumbnail while
// seeking: on the film itself, and in a list where it asks for it, by its copy and width.
func TestAnAppReadsAFilmsTrickplay(t *testing.T) {
	st, ada, heat, copyID := aFilm(t)
	versions, err := st.Versions(t.Context(), []uuid.UUID{heat})
	if err != nil {
		t.Fatal(err)
	}
	// 150 thumbnails, ten seconds apart, a hundred to a sheet: two sheets, the second half full.
	sheets := store.Trickplay{Width: 320, Height: 180, IntervalMS: 10_000, Columns: 10, Rows: 10, Thumbnails: 150}
	if err := st.SavePreviews(t.Context(), versions[heat][0].Files[0].ID, nil, &sheets); err != nil {
		t.Fatal(err)
	}
	api := New(slog.New(slog.DiscardHandler), domain.Info{ID: uuid.NewV7().String(), Name: "Den"}, Services{
		Auth: profiles{"pst_ada": ada}, Catalogue: st,
	})
	const header = `MediaBrowser Client="Jellyfin Web", Token="pst_ada"`
	trickplay := func(target string) map[string]map[string]trickplayInfo {
		t.Helper()
		w := serve(api, http.MethodGet, target, header, "")
		var it struct {
			Items     []item
			Trickplay map[string]map[string]trickplayInfo
		}
		if err := json.Unmarshal(w.Body.Bytes(), &it); w.Code != http.StatusOK || err != nil {
			t.Fatalf("%s: %d %s", target, w.Code, w.Body)
		}
		if it.Items == nil {
			return it.Trickplay
		}
		var got map[string]map[string]trickplayInfo
		if b, err := json.Marshal(it.Items[0].Trickplay); err != nil || json.Unmarshal(b, &got) != nil {
			t.Fatal(err)
		}
		return got
	}
	want := trickplayInfo{Width: 320, Height: 180, TileWidth: 10, TileHeight: 10, ThumbnailCount: 150, Interval: 10_000}
	for _, target := range []string{"/Items/" + guid(heat), "/Items?recursive=true&includeItemTypes=Movie&fields=Trickplay"} {
		if got := trickplay(target); got[guid(copyID)]["320"] != want {
			t.Errorf("%s: trickplay %v, want %+v under the copy and its width", target, got, want)
		}
	}
	if got := trickplay("/Items?recursive=true&includeItemTypes=Movie&fields=MediaSources"); got != nil {
		t.Errorf("a list that did not ask for trickplay: %v", got)
	}
}
