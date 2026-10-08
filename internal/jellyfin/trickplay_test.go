//go:build integration

package jellyfin

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"path"
	"strings"
	"testing"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/analysis"
	"github.com/olivertgwalton/photon-server/internal/blob"
	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// aFilmWithTrickplay is aFilm with its thumbnails made: 150 of them, ten seconds apart, a hundred
// to a sheet, so two sheets, the second half full; the second is kept.
func aFilmWithTrickplay(t *testing.T) (*API, uuid.UUID, uuid.UUID) {
	t.Helper()
	st, ada, heat, copyID := aFilm(t)
	versions, err := st.Versions(t.Context(), []uuid.UUID{heat})
	if err != nil {
		t.Fatal(err)
	}
	part := versions[heat][0].Files[0].ID
	sheets := store.Trickplay{Width: 320, Height: 180, IntervalMS: 10_000, Columns: 10, Rows: 10, Thumbnails: 150}
	if err := st.SavePreviews(t.Context(), part, nil, &sheets); err != nil {
		t.Fatal(err)
	}
	dir, err := blob.OpenDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dir.Close() })
	if err := dir.Put(t.Context(), path.Join(part.String(), "trickplay", "1.jpg"), strings.NewReader("sheet 1")); err != nil {
		t.Fatal(err)
	}
	api := New(slog.New(slog.DiscardHandler), domain.Info{ID: uuid.NewV7().String(), Name: "Den"}, Services{
		Auth: profiles{"pst_ada": ada}, Catalogue: st, Playing: st, Previews: st, PreviewFiles: analysis.NewPreviews(dir),
	})
	return api, heat, copyID
}

// An app reads a film's trickplay, as Jellyfin's web app and Swiftfin do to show a thumbnail while
// seeking: on the film itself, and in a list where it asks for it, by its copy and width.
func TestAnAppReadsAFilmsTrickplay(t *testing.T) {
	api, heat, copyID := aFilmWithTrickplay(t)
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

// An app seeks through a film's thumbnails as Jellyfin's web app does: it reads the playlist of
// its sheets at the width trickplay gave, and fetches each sheet the playlist names, with the
// token the playlist carries.
func TestAnAppFetchesAFilmsTrickplaySheets(t *testing.T) {
	api, heat, copyID := aFilmWithTrickplay(t)
	base := "/Videos/" + guid(heat) + "/Trickplay/320/"
	w := serve(api, http.MethodGet, base+"tiles.m3u8?MediaSourceId="+guid(copyID)+"&ApiKey=pst_ada", "", "")
	want := "#EXTM3U\n#EXT-X-TARGETDURATION:1000\n#EXT-X-VERSION:7\n#EXT-X-MEDIA-SEQUENCE:1\n#EXT-X-PLAYLIST-TYPE:VOD\n#EXT-X-IMAGES-ONLY\n" +
		"#EXTINF:1000,\n#EXT-X-TILES:RESOLUTION=320x180,LAYOUT=10x10,DURATION=10\n0.jpg?ApiKey=pst_ada&MediaSourceId=" + guid(copyID) + "\n" +
		"#EXTINF:500,\n#EXT-X-TILES:RESOLUTION=320x180,LAYOUT=10x10,DURATION=10\n1.jpg?ApiKey=pst_ada&MediaSourceId=" + guid(copyID) + "\n" +
		"#EXT-X-ENDLIST\n"
	if w.Code != http.StatusOK || w.Body.String() != want {
		t.Errorf("the playlist: %d\n%s\nwant\n%s", w.Code, w.Body, want)
	}
	if w := serve(api, http.MethodGet, base+"1.jpg?ApiKey=pst_ada&MediaSourceId="+guid(copyID), "", ""); w.Code != http.StatusOK ||
		w.Body.String() != "sheet 1" || w.Header().Get("Content-Type") != "image/jpeg" {
		t.Errorf("the second sheet: %d %s %q", w.Code, w.Header().Get("Content-Type"), w.Body)
	}
	// The copy photon would play, where the app names none.
	if w := serve(api, http.MethodGet, "/videos/"+guid(heat)+"/trickplay/320/1.jpg?ApiKey=pst_ada", "", ""); w.Code != http.StatusOK {
		t.Errorf("a sheet of the copy photon would play: %d", w.Code)
	}
	for _, target := range []string{
		base + "2.jpg", base + "0.jpg", "/Videos/" + guid(heat) + "/Trickplay/640/tiles.m3u8",
		base + "tiles.m3u8?MediaSourceId=" + guid(uuid.NewV7()),
	} {
		if w := serve(api, http.MethodGet, target, `MediaBrowser Token="pst_ada"`, ""); w.Code != http.StatusNotFound {
			t.Errorf("%s: %d, want 404", target, w.Code)
		}
	}
	if w := serve(api, http.MethodGet, base+"1.jpg", "", ""); w.Code != http.StatusUnauthorized {
		t.Errorf("a sheet without a token: %d, want 401", w.Code)
	}
}
